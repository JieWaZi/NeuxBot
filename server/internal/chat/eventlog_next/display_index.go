package eventlog_next

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// displayPosition is a rebuildable projection of currently effective native operations.
type displayPosition struct {
	// Stream identifies the public Chat.
	Stream int64 `gorm:"primaryKey;autoIncrement:false;index:conversation_display_lane,priority:1"`
	// MessageRef identifies the selected public message.
	MessageRef int64 `gorm:"primaryKey;autoIncrement:false;index:conversation_display_lane,priority:2"`
	// Bucket is the stable physical native bucket, independent of SQLite stream IDs.
	Bucket int `gorm:"primaryKey;autoIncrement:false;index:conversation_display_lane,priority:3"`
	// NativeCursor selects the committed native row or terminal.
	NativeCursor int64 `gorm:"primaryKey;autoIncrement:false"`
	// Lane is the immutable first cursor of the originating native message.
	Lane int64 `gorm:"index:conversation_display_lane,priority:4"`
	// Order is the public link cursor at which this part first became visible.
	Order int64
	// SubOrder preserves native changes within one link.
	SubOrder int64
	// Kind distinguishes text, tools, structured events, classification, and terminals.
	Kind indexKind
	// Content contains only the event type or the latest lane classification.
	Content constants.RuntimeEventType
	// LaneText identifies text affected by a full replacement of this native message.
	LaneText bool
	// Tool identifies the snapshot whose current version this row locates.
	Tool string
}

func (displayPosition) TableName() string { return "conversation_display" }

func nativeBucketPath(bucket int) string {
	return filepath.Join(nativeDirectory, fmt.Sprintf("%02x.jsonl", bucket))
}

func indexDisplay(db *gorm.DB, stream int64, rows []rowIndex) error {
	for _, row := range rows {
		if row.Project == nil {
			continue
		}
		m := row.Project
		var bucket int
		if m.Fragments != nil {
			bucket = nativeShard(m.Fragments.Key.Session)
		} else {
			bucket = nativeShard(m.Result.Session)
		}
		var coverage streamCoverage
		if err := db.Where("path = ?", nativeBucketPath(bucket)).Limit(1).Find(&coverage).Error; err != nil {
			return err
		}
		if coverage.Stream == 0 {
			return status_error.ChatLogIndexCorrupted
		}
		var positions []rowIndex
		if m.Fragments != nil {
			r := m.Fragments
			identity, lane, err := findNativeRefs(db, coverage.Stream, nativeRowID(r.Key), r.MessageID)
			if err != nil {
				return err
			}
			if err := db.Where("stream = ? AND cursor BETWEEN ? AND ?", coverage.Stream, r.First, r.Last).
				Order("cursor").Find(&positions).Error; err != nil {
				return err
			}
			if int64(len(positions)) != r.Last-r.First+1 {
				return status_error.ChatLogIndexCorrupted
			}
			for _, pos := range positions {
				if pos.NativeRef != identity || pos.LaneRef != lane {
					return status_error.ChatLogIndexCorrupted
				}
			}
		} else {
			identity, _, err := findNativeRefs(db, coverage.Stream, nativeRowID(*m.Result), "")
			if err != nil {
				return err
			}
			if err := db.Table("conversation_positions INDEXED BY conversation_row_native").
				Where("stream = ? AND native_ref <> 0 AND native_ref = ? AND kind = ?", coverage.Stream, identity, indexNativeTerminal).
				Limit(1).Find(&positions).Error; err != nil {
				return err
			}
			if len(positions) != 1 || positions[0].LaneRef == 0 {
				return status_error.ChatLogIndexCorrupted
			}
		}
		revision := m.Revision
		for _, pos := range positions {
			revision = max(revision, pos.Revision)
			d := displayPosition{Stream: stream, MessageRef: row.MessageRef, Bucket: bucket, NativeCursor: pos.Cursor, Lane: pos.LaneRef,
				Order: row.Cursor, SubOrder: pos.Cursor, Kind: pos.Kind, Content: pos.Content, Tool: pos.Tool,
				LaneText: pos.Kind == indexNativeTerminal || (pos.Kind == indexNativeTextAppend || pos.Kind == indexNativeTextReplace) && isClassifiableText(pos.Content)}
			if err := publishDisplayPosition(db, d); err != nil {
				return err
			}
		}
		if revision > 0 {
			if err := db.Model(&messageAnchor{}).Where("stream = ? AND message = ? AND revision < ?", stream, indexIdentity(m.MessageID), revision).
				Update("revision", revision).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func publishDisplayPosition(db *gorm.DB, d displayPosition) error {
	lane := func() *gorm.DB {
		return db.Model(&displayPosition{}).Where("stream = ? AND message_ref = ? AND bucket = ? AND lane = ?", d.Stream, d.MessageRef, d.Bucket, d.Lane)
	}
	var replaced func() *gorm.DB
	switch {
	case d.LaneText:
		// A classified lane treats its text fragments as one versioned body.
		// Otherwise a replacement preserves explicitly identified commentary.
		var classification displayPosition
		if err := lane().Where("kind = ?", indexNativeClassification).Limit(1).Find(&classification).Error; err != nil {
			return err
		}
		if classification.NativeCursor != 0 || isFinalText(d.Content) || d.Kind == indexNativeTerminal {
			replaced = func() *gorm.DB {
				q := lane().Where("lane_text").Where("kind IN ?", []indexKind{indexNativeTextReplace, indexNativeTerminal})
				if classification.NativeCursor == 0 {
					q = q.Where("content IN ?", nativeTextContents(false))
				}
				return q
			}
			if superseded, err := laterDisplayExists(replaced(), d.NativeCursor); err != nil || superseded {
				return err
			}
			if d.Kind == indexNativeTextReplace || d.Kind == indexNativeTerminal {
				previous := func() *gorm.DB {
					q := lane().Where("lane_text AND native_cursor < ?", d.NativeCursor)
					if classification.NativeCursor == 0 {
						q = q.Where("content IN ?", nativeTextContents(false))
					}
					return q
				}
				var first displayPosition
				if err := previous().Order("\"order\", sub_order").Limit(1).Find(&first).Error; err != nil {
					return err
				}
				if first.Order > 0 {
					d.Order, d.SubOrder = first.Order, first.SubOrder
				}
				if err := previous().Delete(&displayPosition{}).Error; err != nil {
					return err
				}
			}
		}
	case d.Kind == indexNativeTool:
		replaced = func() *gorm.DB {
			return lane().Where("kind = ? AND tool = ? AND content = ?", d.Kind, d.Tool, d.Content)
		}
	case d.Kind == indexNativeClassification:
		replaced = func() *gorm.DB { return lane().Where("kind = ?", d.Kind) }
	}
	if d.Kind == indexNativeTool || d.Kind == indexNativeClassification {
		if superseded, err := laterDisplayExists(replaced(), d.NativeCursor); err != nil || superseded {
			return err
		}
		if err := replaced().Where("native_cursor < ?", d.NativeCursor).Delete(&displayPosition{}).Error; err != nil {
			return err
		}
		if d.Kind == indexNativeClassification {
			// Once the lane is explicitly classified, a preceding replacement
			// also supersedes any formerly separate commentary in that lane.
			var boundary displayPosition
			if err := lane().Where("lane_text").Where("kind IN ?", []indexKind{indexNativeTextReplace, indexNativeTerminal}).
				Order("native_cursor DESC").Limit(1).Find(&boundary).Error; err != nil {
				return err
			}
			if boundary.NativeCursor > 0 {
				if err := lane().Where("lane_text AND native_cursor < ?", boundary.NativeCursor).
					Delete(&displayPosition{}).Error; err != nil {
					return err
				}
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&d).Error
}

func laterDisplayExists(query *gorm.DB, cursor int64) (bool, error) {
	var winner displayPosition
	err := query.Select("native_cursor").Where("native_cursor > ?", cursor).Limit(1).Find(&winner).Error
	return winner.NativeCursor > 0, err
}

func (i *streamIndex) displayForMessages(refs []int64) (map[int64][]displayPosition, error) {
	out := make(map[int64][]displayPosition, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	err := i.query(func(db *gorm.DB) error {
		const batch = 64
		for start := 0; start < len(refs); start += batch {
			end := min(start+batch, len(refs))
			var sql strings.Builder
			args := make([]any, 0, (end-start)*2)
			for j, ref := range refs[start:end] {
				if j > 0 {
					sql.WriteString(" UNION ALL ")
				}
				sql.WriteString("SELECT * FROM conversation_display WHERE stream = ? AND message_ref = ?")
				args = append(args, i.coverage.Stream, ref)
			}
			sql.WriteString(" ORDER BY message_ref, \"order\", sub_order")
			var rows []displayPosition
			if err := db.Raw(sql.String(), args...).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				out[row.MessageRef] = append(out[row.MessageRef], row)
			}
		}
		return nil
	})
	return out, err
}
