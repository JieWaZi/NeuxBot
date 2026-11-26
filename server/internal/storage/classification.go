package storage

import (
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func classifyFile(root *scanRoot, relative string) FileEntry {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if root.scope&dataRoot != 0 {
		if category := programDataCategory(root, parts); category != constants.StorageCategoryOther {
			return FileEntry{Category: category}
		}
	}
	if root.scope&managedRoot != 0 {
		return managedFileEntry(root, parts)
	}
	return FileEntry{Category: constants.StorageCategoryOther}
}

func programDataCategory(root *scanRoot, parts []string) constants.StorageCategory {
	head := parts[0]
	if len(parts) == 1 {
		database := root.databaseName
		if database == "" {
			database = "bot.db"
		}
		switch {
		case sqliteFiles(head, database) || head == "integration-api.json" || head == "storage-policy.json":
			return constants.StorageCategoryAppData
		case sqliteFiles(head, "memory.db"):
			return constants.StorageCategoryKnowledge
		default:
			return constants.StorageCategoryOther
		}
	}

	switch head {
	case "electron":
		if len(parts) > 2 {
			switch parts[1] {
			case "Cache", "Code Cache", "GPUCache", "DawnGraphiteCache", "DawnWebGPUCache":
				return constants.StorageCategoryCache
			}
		}
		return constants.StorageCategoryAppData
	case "knowledge":
		if len(parts) == 2 && sqliteFiles(parts[1], "knowledge.db") {
			return constants.StorageCategoryKnowledge
		}
	case "skills":
		if len(parts) > 2 && parts[1] == "versions" {
			return constants.StorageCategoryPrograms
		}
	case constants.AgentCLISystemSkillsDirectory, "runtime-installations":
		return constants.StorageCategoryPrograms
	case "ai-accounts", "account-workspaces":
		return constants.StorageCategoryAccounts
	case "agent-contexts":
		return constants.StorageCategoryTemporary
	case "logs":
		return constants.StorageCategoryLogs
	}
	if len(parts) > 2 && (head == "plugins" || (root.pluginAlias && head == "Plugins")) {
		switch parts[1] {
		case "targets":
			return constants.StorageCategoryCache
		case "content", "data":
			return constants.StorageCategoryPrograms
		}
	}
	if strings.HasPrefix(head, "web-uploads-") {
		return constants.StorageCategoryTemporary
	}
	return constants.StorageCategoryOther
}

func managedFileEntry(root *scanRoot, parts []string) FileEntry {
	other := FileEntry{Category: constants.StorageCategoryOther}
	if len(parts) == 1 {
		if sqliteFiles(parts[0], "conversation-index.db") {
			return FileEntry{Category: constants.StorageCategoryAppData}
		}
		return other
	}

	switch parts[0] {
	case "Conversations":
		if len(parts) < 3 {
			return other
		}
		entry := FileEntry{Category: constants.StorageCategoryOther, ChatID: parts[1]}
		if len(parts) >= 4 {
			switch parts[2] {
			case "log", constants.ExecutionWorkspaceAttachmentsDirectory, "file-results":
				entry.Category = constants.StorageCategoryConversations
				entry.Folder = filepath.Join(root.path, parts[0], parts[1], parts[2])
			}
		}
		return entry
	case "Agents":
		if parts[1] == constants.WakerCopyStagingDirectoryName || root.pendingWakerCopies[parts[1]] {
			return FileEntry{Category: constants.StorageCategoryRecovery}
		}
		if len(parts) >= 3 {
			return FileEntry{Category: constants.StorageCategoryWorkFiles, WakerID: parts[1]}
		}
	case constants.NativeConversationDirectoryName:
		return FileEntry{Category: constants.StorageCategorySharedNative}
	case constants.KnowledgeFilesDirectoryName:
		if len(parts) > 2 && parts[1] == "raw" {
			return FileEntry{Category: constants.StorageCategoryKnowledge}
		}
	case "Workspaces", constants.WorkflowWorkspaceDirectoryName, constants.WorkflowFileDirectoryName:
		return FileEntry{Category: constants.StorageCategoryWorkFiles}
	case constants.IMFilesDirectoryName:
		if len(parts) > 2 && (parts[1] == "inbox" || parts[1] == "deliveries") {
			return FileEntry{Category: constants.StorageCategoryConversations}
		}
	case constants.ChatForkDirectoryName, "Deletions", "Provisioning":
		return FileEntry{Category: constants.StorageCategoryRecovery}
	}
	if len(parts) > 2 && (parts[0] == "Plugins" || (root.pluginAlias && parts[0] == "plugins")) {
		switch parts[1] {
		case "catalog", "details", "archives":
			return FileEntry{Category: constants.StorageCategoryCache}
		}
	}
	return other
}

func sqliteFiles(name, database string) bool {
	switch strings.TrimPrefix(name, database) {
	case "", "-journal", "-wal", "-shm":
		return strings.HasPrefix(name, database)
	default:
		return false
	}
}
