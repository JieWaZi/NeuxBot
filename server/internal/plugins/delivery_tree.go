package plugins

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ValidatePreparationBoundary 在创建目录前按真实路径拒绝原包内部暂存。
func ValidatePreparationBoundary(source, parent string) error {
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return storageFailure(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return storageFailure(err)
	}
	destination, err := filepath.Abs(parent)
	if err != nil {
		return storageFailure(err)
	}
	destination, err = plannedPluginDirectory(destination)
	if err != nil {
		return err
	}
	if codexPathWithin(root, destination) {
		return status_error.PluginContentInvalid
	}
	return nil
}

// deliveryNode 保存一次捕获的目录类型与链接原文字节，不跟随链接读取外部树。
type deliveryNode struct {
	// mode 标识目录、普通文件或符号链接。
	mode os.FileMode
	// link 是符号链接的原始声明；普通条目为空。
	link string
}

// deliveryGraph 以源根坐标解析被冻结的目录与链接声明。
type deliveryGraph struct {
	// root 是原始来源的真实根，仅用于复制原始目录。
	root string
	// aliases 是调用方入口与真实根两个已知坐标，不推断任意包外目录别名。
	aliases []string
	// nodes 是当前待校验树的捕获条目；复制后替换为冻结副本条目。
	nodes map[string]deliveryNode
}

func captureDeliveryGraph(root string) (deliveryGraph, error) {
	original, err := filepath.Abs(root)
	if err != nil {
		return deliveryGraph{}, storageFailure(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return deliveryGraph{}, storageFailure(err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return deliveryGraph{}, storageFailure(err)
	}
	nodes, err := readDeliveryNodes(canonical)
	if err != nil {
		return deliveryGraph{}, err
	}
	return deliveryGraph{root: canonical, aliases: []string{original, canonical}, nodes: nodes}, nil
}

func readDeliveryNodes(root string) (map[string]deliveryNode, error) {
	nodes := map[string]deliveryNode{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return storageFailure(err)
		}
		if entry.Name() == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return status_error.PluginContentInvalid
		}
		node := deliveryNode{mode: entry.Type()}
		if entry.Type()&os.ModeSymlink != 0 {
			node.link, err = os.Readlink(path)
			if err != nil {
				return storageFailure(err)
			}
		}
		nodes[relative] = node
		return nil
	})
	return nodes, err
}

func (graph deliveryGraph) absoluteParts(link string) ([]string, error) {
	for _, alias := range graph.aliases {
		if link == alias {
			return nil, nil
		}
		if strings.HasPrefix(link, alias+string(filepath.Separator)) {
			return strings.Split(link[len(alias)+1:], string(filepath.Separator)), nil
		}
	}
	return nil, status_error.PluginContentInvalid
}

func (graph deliveryGraph) resolve(relative string) (string, error) {
	queue := strings.Split(relative, string(filepath.Separator))
	current := []string{}
	links := 0
	for len(queue) > 0 {
		part := queue[0]
		queue = queue[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(current) == 0 {
				return "", status_error.PluginContentInvalid
			}
			current = current[:len(current)-1]
			continue
		}
		path := filepath.Join(append(append([]string{}, current...), part)...)
		node, exists := graph.nodes[path]
		if !exists {
			return "", status_error.PluginContentInvalid
		}
		if node.mode&os.ModeSymlink != 0 {
			links++
			if links > len(graph.nodes)*2+512 {
				return "", status_error.PluginContentInvalid
			}
			target := strings.Split(node.link, string(filepath.Separator))
			if filepath.IsAbs(node.link) {
				var err error
				target, err = graph.absoluteParts(node.link)
				if err != nil {
					return "", err
				}
				current = nil
			}
			queue = append(target, queue...)
			continue
		}
		current = append(current, part)
		if len(queue) > 0 && !node.mode.IsDir() {
			return "", status_error.PluginContentInvalid
		}
	}
	if len(current) == 0 {
		return ".", nil
	}
	return filepath.Join(current...), nil
}

func (graph deliveryGraph) linkTargets() (map[string]string, error) {
	targets := map[string]string{}
	for path, node := range graph.nodes {
		if node.mode&os.ModeSymlink == 0 {
			continue
		}
		resolved, err := graph.resolve(path)
		if err != nil {
			return nil, err
		}
		targets[path] = resolved
	}
	return targets, nil
}

// CopyDeliveryTree 复制经校验的完整树，并将合法内部绝对链接重定位为相对链接。
func CopyDeliveryTree(source, destination string) error {
	return CopyDeliveryTreeWithDigest(source, destination, "")
}

// CopyDeliveryTreeWithDigest 将规范化前复制字节直接绑定已接纳摘要；链接解析只消费冻结图。
func CopyDeliveryTreeWithDigest(source, destination, expected string) error {
	graph, err := captureDeliveryGraph(source)
	if err != nil {
		return err
	}
	if err := ValidatePreparationBoundary(source, destination); err != nil {
		return err
	}
	if _, err := graph.linkTargets(); err != nil {
		return err
	}
	if expected == "" {
		expected, err = pluginArchive.Digest(graph.root)
		if err != nil {
			return err
		}
	}
	if err := pluginArchive.CopyDirectory(graph.root, destination); err != nil {
		return err
	}
	copied, err := pluginArchive.Digest(destination)
	if err != nil {
		return err
	}
	if copied != expected {
		return status_error.PluginVersionConflict
	}
	graph.nodes, err = readDeliveryNodes(destination)
	if err != nil {
		return err
	}
	targets, err := graph.linkTargets()
	if err != nil {
		return err
	}
	for path, target := range targets {
		if !filepath.IsAbs(graph.nodes[path].link) {
			continue
		}
		output := filepath.Join(destination, path)
		link, err := filepath.Rel(filepath.Dir(output), filepath.Join(destination, target))
		if err != nil {
			return status_error.PluginContentInvalid
		}
		if err := os.Remove(output); err != nil {
			return storageFailure(err)
		}
		if err := os.Symlink(link, output); err != nil {
			return storageFailure(err)
		}
	}
	return validateLinks(destination)
}

// requireActualReachability 仅拒绝操作系统不可读的来源或最终目标，不参与规范化映射。
func (graph deliveryGraph) requireActualReachability() error {
	for path, node := range graph.nodes {
		if node.mode&os.ModeSymlink != 0 {
			if _, err := os.Stat(filepath.Join(graph.root, path)); err != nil {
				return status_error.PluginContentInvalid
			}
		}
	}
	return nil
}

func hasPositionDependentDeliveryLinks(root string) (bool, error) {
	graph, err := captureDeliveryGraph(root)
	if err != nil {
		return false, err
	}
	targets, err := graph.linkTargets()
	if err != nil {
		return false, err
	}
	if err := graph.requireActualReachability(); err != nil {
		return false, err
	}
	for path := range targets {
		if filepath.IsAbs(graph.nodes[path].link) {
			return true, nil
		}
	}
	return false, nil
}

// writeTargetFile 以目录能力和目录项替换写入生成文件，不跟随任何生成路径别名。
func writeTargetFile(directory, path string, body []byte) error {
	if !safeRelative(path) {
		return status_error.PluginContentInvalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return storageFailure(err)
	}
	defer root.Close()
	current := ""
	for _, part := range splitDirectory(filepath.Dir(path)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if err := root.Mkdir(current, 0700); err != nil {
				return storageFailure(err)
			}
			continue
		}
		if err != nil {
			return storageFailure(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return status_error.PluginContentInvalid
		}
	}
	temporary, err := os.CreateTemp(directory, ".target-write-")
	if err != nil {
		return storageFailure(err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return storageFailure(err)
	}
	if err := temporary.Close(); err != nil {
		return storageFailure(err)
	}
	if err := root.Rename(filepath.Base(temporary.Name()), path); err != nil {
		return storageFailure(err)
	}
	return nil
}

func splitDirectory(path string) []string {
	if path == "." {
		return nil
	}
	parts := []string{}
	for path != "." {
		parts = append([]string{filepath.Base(path)}, parts...)
		path = filepath.Dir(path)
	}
	return parts
}
