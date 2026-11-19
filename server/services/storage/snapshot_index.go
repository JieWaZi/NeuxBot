package storage

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	filesystem "github.com/JieWaZi/NeuxBot/server/internal/storage"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// RankedEntity 汇总属于一位伙伴或单个会话的逻辑文件字节。
type RankedEntity struct {
	// ID 是实体身份。
	ID string
	// Title 是会话当前标题。
	Title string
	// WakerID 是会话所属伙伴。
	WakerID string
	// Bytes 是实体逻辑总占用。
	Bytes int64
	// WorkspaceBytes 是伙伴空间逻辑占用。
	WorkspaceBytes int64
	// InternalBytes 是伙伴使用的受管资源逻辑占用。
	InternalBytes int64
	// ChatBytes 是会话私有文件逻辑占用。
	ChatBytes int64
	// FileCount 是可管理文件或可管理附件的数量。
	FileCount int
	// ChatCount 是伙伴可见且已测量的会话数。
	ChatCount int
}

// IndexedFile 保存已核验路径及其展示所属，供列表和删除复用同一证据。
type IndexedFile struct {
	filesystem.UserFile
	// WakerID 是实际拥有此项的伙伴。
	WakerID string
	// ChatTitle 是会话快照标题，工作文件为空。
	ChatTitle string
	// searchName 是用于匹配相对文件路径的小写文本。
	searchName string
}

type snapshotIndex struct {
	// partners 是稳定按大小排序的伙伴列表。
	partners []RankedEntity
	// chats 是稳定按大小排序的普通会话列表。
	chats []RankedEntity
	// partnerChats 为单个伙伴复用全局同一排名顺序。
	partnerChats map[string][]RankedEntity
	// files 按准确路径查找单个已核验文件。
	files map[string]*IndexedFile
	// partnerFiles 为伙伴空间及其可见附件建立独立明细桶。
	partnerFiles map[string]*fileBucket
	// chatFiles 仅保存该会话的附件明细。
	chatFiles map[string]*fileBucket
	// filesComplete 表示本代可管理文件的归属检查完整。
	filesComplete bool
}

type fileBucket struct {
	// latest 按修改时间排序，每个可核验路径只出现一次。
	latest []*IndexedFile
	// largest 按逻辑文件大小排序，复用 latest 中的条目。
	largest []*IndexedFile
	// bytes 汇总分组中路径的逻辑大小，硬链接路径不丢失。
	bytes int64
}

func entityFilePaths(measurement filesystem.Measurement) func(func(filesystem.FileEntry) bool) {
	return func(yield func(filesystem.FileEntry) bool) {
		for _, group := range [][]filesystem.FileEntry{measurement.Entries, measurement.FileAliases} {
			for _, entry := range group {
				if !yield(entry) {
					return
				}
			}
		}
	}
}

func (s *Snapshot) indexEntities(ctx context.Context, inherited filesystem.InheritedAttachments) error {
	if s.ChatMetadataError != nil {
		return nil
	}
	s.index = snapshotIndex{
		files: map[string]*IndexedFile{}, partnerFiles: map[string]*fileBucket{}, chatFiles: map[string]*fileBucket{},
		partnerChats: map[string][]RankedEntity{},
	}
	visible := map[string]models.Chat{}
	for _, chat := range s.ChatMetadata {
		if !chat.HiddenFromWakerHistory && chat.WakerID != "" {
			visible[chat.ID] = chat
		}
	}
	inspectInherited := inherited
	if inherited != nil {
		inspectInherited = func(ctx context.Context, id string) (map[string]string, error) {
			if visible[id].ForkSourceChatID == "" {
				return nil, nil
			}
			return inherited(ctx, id)
		}
	}
	files, complete := s.Measurement.InspectUserFiles(ctx, func(id string) bool { _, ok := visible[id]; return ok }, inspectInherited)
	s.index.filesComplete = complete
	for _, file := range files {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		chat := visible[file.Entry.ChatID]
		waker := file.Entry.WakerID
		if chat.ID != "" {
			waker = chat.WakerID
		}
		indexed := &IndexedFile{UserFile: file, WakerID: waker, ChatTitle: chat.Title, searchName: strings.ToLower(file.RelativePath)}
		s.index.files[file.ID] = indexed
		addBucketFile(s.index.partnerFiles, waker, indexed)
		if chat.ID != "" {
			addBucketFile(s.index.chatFiles, chat.ID, indexed)
		}
	}
	for _, buckets := range []map[string]*fileBucket{s.index.partnerFiles, s.index.chatFiles} {
		for _, bucket := range buckets {
			if ctx.Err() != nil {
				return status_error.OperationCanceled
			}
			bucket.sort()
		}
	}

	partners := map[string]RankedEntity{}
	for _, chat := range visible {
		usage := s.Chats[chat.ID]
		count := 0
		if bucket := s.index.chatFiles[chat.ID]; bucket != nil {
			count = len(bucket.latest)
		}
		s.index.chats = append(s.index.chats, RankedEntity{ID: chat.ID, Title: chat.Title, WakerID: chat.WakerID, Bytes: usage.Bytes, ChatBytes: usage.Bytes, FileCount: count})
		partner := partners[chat.WakerID]
		partner.ID = chat.WakerID
		partner.Bytes += usage.Bytes
		partner.ChatBytes += usage.Bytes
		partner.ChatCount++
		partners[chat.WakerID] = partner
	}
	for entry := range entityFilePaths(s.Measurement) {
		if ctx.Err() != nil {
			return status_error.OperationCanceled
		}
		if entry.WakerID == "" || entry.ChatID != "" {
			continue
		}
		partner := partners[entry.WakerID]
		partner.ID = entry.WakerID
		partner.Bytes += entry.Bytes
		if entry.Category == constants.StorageCategoryPrograms {
			partner.InternalBytes += entry.Bytes
		} else {
			partner.WorkspaceBytes += entry.Bytes
		}
		partners[entry.WakerID] = partner
	}
	for _, partner := range partners {
		if bucket := s.index.partnerFiles[partner.ID]; bucket != nil {
			partner.FileCount = len(bucket.latest)
		}
		s.index.partners = append(s.index.partners, partner)
	}
	slices.SortFunc(s.index.chats, compareRank)
	slices.SortFunc(s.index.partners, compareRank)
	for _, chat := range s.index.chats {
		s.index.partnerChats[chat.WakerID] = append(s.index.partnerChats[chat.WakerID], chat)
	}
	return nil
}

func compareRank(a, b RankedEntity) int {
	if comparison := cmp.Compare(b.Bytes, a.Bytes); comparison != 0 {
		return comparison
	}
	return strings.Compare(a.ID, b.ID)
}

func addBucketFile(buckets map[string]*fileBucket, id string, file *IndexedFile) {
	bucket := buckets[id]
	if bucket == nil {
		bucket = &fileBucket{}
		buckets[id] = bucket
	}
	bucket.latest = append(bucket.latest, file)
	bucket.bytes += file.Entry.Bytes
}

func (b *fileBucket) sort() {
	latest := func(a, b *IndexedFile) int {
		if comparison := b.Entry.ModifiedAt.Compare(a.Entry.ModifiedAt); comparison != 0 {
			return comparison
		}
		return strings.Compare(a.ID, b.ID)
	}
	slices.SortFunc(b.latest, latest)
	b.largest = slices.Clone(b.latest)
	slices.SortStableFunc(b.largest, func(a, b *IndexedFile) int { return cmp.Compare(b.Entry.Bytes, a.Entry.Bytes) })
}

// RankedEntities 返回不可修改的同代排序快照，切页时调用者复制所需的行。
func (s Snapshot) RankedEntities(kind, wakerID string) []RankedEntity {
	if kind == "wakers" {
		return s.index.partners
	}
	if wakerID != "" {
		return s.index.partnerChats[wakerID]
	}
	return s.index.chats
}

// UserFilesPage 读取单个所有者的稳定明细页和匹配当前检索的逻辑使用量。
func (s Snapshot) UserFilesPage(ctx context.Context, kind, id, search string, largest bool, offset, limit int) ([]*IndexedFile, int, int64, bool, error) {
	bucket := s.index.partnerFiles[id]
	if kind == "chats" {
		bucket = s.index.chatFiles[id]
	}
	complete := s.Measurement.Complete && s.index.filesComplete
	if bucket == nil {
		return nil, 0, 0, complete, nil
	}
	rows := bucket.latest
	if largest {
		rows = bucket.largest
	}
	if search == "" {
		start := min(offset, len(rows))
		return rows[start:min(start+limit, len(rows))], len(rows), bucket.bytes, complete, nil
	}
	var page []*IndexedFile
	var total int
	var totalBytes int64
	for _, row := range rows {
		if ctx.Err() != nil {
			return nil, 0, 0, false, status_error.OperationCanceled
		}
		if strings.Contains(row.searchName, search) {
			if total >= offset && len(page) < limit {
				page = append(page, row)
			}
			total++
			totalBytes += row.Entry.Bytes
		}
	}
	return page, total, totalBytes, complete, nil
}

// FindUserFile 确认请求范围包含这条已核验路径。
func (s Snapshot) FindUserFile(kind, owner, id string) (*IndexedFile, bool) {
	entry := s.index.files[id]
	if entry == nil {
		return nil, false
	}
	if kind == "chats" {
		return entry, entry.Entry.ChatID == owner
	}
	return entry, entry.WakerID == owner
}
