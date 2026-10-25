// Package builtin 安装随 Core 打包的技能，并按角色声明初始化伙伴启用关系。
package builtin

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

//go:embed bundle.generated.json
var bundle []byte

// definition 保存仓库发布清单和完整技能资源，不依赖运行时的源码目录。
type definition struct {
	// System 表示每次受管执行必备、不进入普通技能编辑关系的系统技能。
	System bool `json:"system,omitempty"`
	// ID 是跨应用重启保持稳定的技能身份。
	ID string `json:"id"`
	// Directory 是技能声明名及仓库内目录名称。
	Directory string `json:"directory"`
	// Version 是首次安装和内容刷新时采用的最低发布版本。
	Version string `json:"version"`
	// Files 保存该技能的全部资源和执行位。
	Files []resource `json:"files"`
}

// resource 是生成器打包的一个普通文件。
type resource struct {
	// Path 是相对于技能目录的安全路径。
	Path string `json:"path"`
	// Data 是 JSON 中以 Base64 保存的原始字节。
	Data []byte `json:"data"`
	// Executable 表示资源需要保留脚本执行位。
	Executable bool `json:"executable"`
}

// Installer 在文件准备完成后以事务安装或刷新内置技能，保留用户编辑版本。
type Installer struct {
	// DataDirectory 是技能版本所在的应用数据目录。
	DataDirectory string
	// Skills 提供技能、版本及伙伴启用关系的持久化能力。
	Skills controllers.SkillController
	// Transactions 原子提交技能包、版本历史及当前版本指针。
	Transactions controllers.TransactionController
}

func definitions() ([]definition, error) {
	var content struct {
		// Skills 是构建时从仓库根 skills/ 生成的目录。
		Skills []definition `json:"skills"`
	}
	if err := json.Unmarshal(bundle, &content); err != nil {
		logx.Errorf("decode builtin skill bundle failed: error=%v", err)
		return nil, status_error.SkillContentInvalid
	}
	return content.Skills, nil
}

// EnsureCatalog 幂等安装内置技能，并将未被用户编辑的技能刷新到随应用发布的内容。
func (installer Installer) EnsureCatalog(ctx context.Context) error {
	items, err := definitions()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.System {
			continue
		}
		if err := installer.ensureDefinition(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (installer Installer) ensureDefinition(ctx context.Context, item definition) error {
	stored, err := installer.Skills.GetPackage(ctx, item.ID)
	exists := err == nil
	var current models.SkillVersion
	if exists {
		if !stored.BuiltIn || stored.Source != constants.SkillSourceBuiltin {
			return status_error.SkillContentInvalid
		}
		current, err = installer.Skills.GetVersion(ctx, stored.CurrentVersionID)
		if err != nil {
			return err
		}
		if current.Operation != string(constants.SkillSourceBuiltin) {
			return nil
		}
	} else {
		var status statuserror.CommonError
		if !errors.As(err, &status) || status.Code() != status_error.SkillNotFound.Code() {
			return err
		}
	}
	staging, err := os.MkdirTemp("", "ally-builtin-skill-")
	if err != nil {
		logx.Errorf("prepare builtin skill staging failed: skill_id=%s error=%v", item.ID, err)
		return status_error.SkillStorageError
	}
	defer os.RemoveAll(staging)
	if err := materialize(item, staging); err != nil {
		return err
	}
	metadata, err := managedskills.ReadMetadata(staging)
	if err != nil {
		return err
	}
	if metadata.Name != item.Directory {
		return status_error.SkillContentInvalid
	}
	digest, err := managedskills.Digest(staging)
	if err != nil {
		return err
	}
	if exists && digest == current.ContentDigest {
		return nil
	}
	number := item.Version
	if exists {
		versions, err := installer.Skills.ListVersions(ctx, item.ID)
		if err != nil {
			return err
		}
		number, err = nextBuiltinVersion(item.Version, versions)
		if err != nil {
			return err
		}
	}
	relative, digest, err := managedskills.CopyVersion(
		installer.DataDirectory,
		item.ID,
		number,
		staging,
	)
	if err != nil {
		return err
	}
	versionID := util.New()
	now := time.Now().UnixMilli()
	skill := models.SkillPackage{
		ID: item.ID, Name: metadata.Name, Description: metadata.Description,
		Scope: constants.SkillScopePublic, Source: constants.SkillSourceBuiltin, BuiltIn: true,
		CurrentVersionID: versionID, CreatedAt: now, UpdatedAt: now,
	}
	version := models.SkillVersion{
		ID: versionID, SkillPackageID: item.ID, Version: number,
		Name: metadata.Name, Description: metadata.Description,
		ContentPath: relative, ContentDigest: digest, Operation: string(constants.SkillSourceBuiltin), CreatedAt: now,
	}
	err = installer.Transactions.Transaction(ctx, func(ctx context.Context) error {
		expected := stored.CurrentVersionID
		if !exists {
			if err := installer.Skills.CreatePackage(ctx, skill); err != nil {
				return err
			}
			expected = versionID
		}
		return installer.Skills.CreateVersion(
			ctx,
			version,
			true,
			expected,
		)
	})
	if err != nil {
		if cleanupErr := managedskills.RemoveVersion(installer.DataDirectory, relative); cleanupErr != nil {
			logx.Errorf("remove uncommitted builtin skill failed: skill_id=%s error=%v", item.ID, cleanupErr)
		}
	}
	return err
}

// nextBuiltinVersion 使用发布版本或历史最高版本的下一个补丁号，避免复用不可变目录。
func nextBuiltinVersion(published string, versions []models.SkillVersion) (string, error) {
	parse := func(value string) ([3]*big.Int, bool) {
		var numbers [3]*big.Int
		parts := strings.Split(value, ".")
		if len(parts) != len(numbers) {
			return numbers, false
		}
		for index, part := range parts {
			number, ok := new(big.Int).SetString(part, 10)
			if !ok || number.Sign() < 0 {
				return numbers, false
			}
			numbers[index] = number
		}
		return numbers, true
	}
	maximum, ok := parse(published)
	if !ok {
		return "", status_error.SkillContentInvalid
	}
	advance := false
	for _, version := range versions {
		numbers, ok := parse(version.Version)
		if !ok {
			return "", status_error.SkillContentInvalid
		}
		comparison := 0
		for index := range maximum {
			if comparison = numbers[index].Cmp(maximum[index]); comparison != 0 {
				break
			}
		}
		if comparison >= 0 {
			maximum, advance = numbers, true
		}
	}
	if advance {
		maximum[2].Add(maximum[2], big.NewInt(1))
	}
	number := maximum[0].String() + "." + maximum[1].String() + "." + maximum[2].String()
	if len(number) > 40 {
		return "", status_error.SkillContentInvalid
	}
	return number, nil
}

func materialize(item definition, directory string) error {
	for _, file := range item.Files {
		if !filepath.IsLocal(file.Path) {
			return status_error.SkillContentInvalid
		}
		target := filepath.Join(directory, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			logx.Errorf("create builtin skill resource directory failed: skill_id=%s error=%v", item.ID, err)
			return status_error.SkillStorageError
		}
		mode := os.FileMode(0o600)
		if file.Executable {
			mode = 0o700
		}
		if err := os.WriteFile(target, file.Data, mode); err != nil {
			logx.Errorf("write builtin skill resource failed: skill_id=%s error=%v", item.ID, err)
			return status_error.SkillStorageError
		}
	}
	return nil
}

// EnableForWaker 只补齐角色声明的默认关系，保留伙伴已保存的停用选择。
func EnableForWaker(
	ctx context.Context,
	catalog controllers.SkillController,
	wakerID string,
	names []string,
) error {
	if len(names) == 0 {
		return nil
	}
	items, err := definitions()
	if err != nil {
		return err
	}
	byName := make(map[string]definition, len(items))
	for _, item := range items {
		byName[item.Directory] = item
	}
	for _, name := range names {
		item, exists := byName[name]
		if !exists {
			return status_error.SkillUnavailable
		}
		skill, err := catalog.GetPackage(ctx, item.ID)
		if err != nil {
			return err
		}
		if skill.CurrentVersionID == "" {
			return status_error.SkillUnavailable
		}
		if err := catalog.EnsureWakerSkill(ctx, models.WakerSkill{
			ID: util.New(), WakerID: wakerID, SkillPackageID: item.ID, Enabled: true,
		}); err != nil {
			return err
		}
	}
	return nil
}
