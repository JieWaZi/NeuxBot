package eventlog_next

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

type nativeIdentityLocator struct {
	// Stream scopes the identity to its durable bucket.
	Stream int64 `gorm:"primaryKey;autoIncrement:false"`
	// Digest addresses the canonical invocation or detached input.
	Digest []byte `gorm:"primaryKey"`
	// Ref is the first committed occurrence of the identity in this stream.
	Ref int64
}

func (nativeIdentityLocator) TableName() string { return "conversation_native_identities" }

type nativeMessageLocator struct {
	// Stream identifies the durable bucket.
	Stream int64 `gorm:"primaryKey;autoIncrement:false"`
	// Identity identifies the invocation owning this message.
	Identity int64 `gorm:"primaryKey;autoIncrement:false"`
	// Message is the original message identity stored once per invocation.
	Message indexIdentity `gorm:"primaryKey"`
	// Ref is the first committed occurrence of the native message in this stream.
	Ref int64
}

func (nativeMessageLocator) TableName() string { return "conversation_native_messages" }

func nativeTextContents(includeCommentary bool) []constants.RuntimeEventType {
	third := constants.RuntimeEventTypeTextDelta
	if includeCommentary {
		third = constants.RuntimeEventTypeCommentaryDelta
	}
	return []constants.RuntimeEventType{"", constants.RuntimeEventTypeTextDelta, third}
}

func findNativeRefs(db *gorm.DB, stream int64, key []byte, message string) (int64, int64, error) {
	var identity nativeIdentityLocator
	if err := db.Where("stream = ? AND digest = ?", stream, key).Limit(1).Find(&identity).Error; err != nil {
		return 0, 0, err
	}
	if message == "" || identity.Ref == 0 {
		return identity.Ref, 0, nil
	}
	var lane nativeMessageLocator
	err := db.Where("stream = ? AND identity = ? AND message = ?", stream, identity.Ref, indexIdentity(message)).Limit(1).Find(&lane).Error
	return identity.Ref, lane.Ref, err
}

func internNativeIdentities(db *gorm.DB, stream int64, rows []rowIndex) error {
	var identities []nativeIdentityLocator
	var keys [][]byte
	refs := make(map[string]int64)
	for _, row := range rows {
		id := string(row.Native)
		if id != "" && refs[id] == 0 {
			refs[id] = row.Cursor
			identities = append(identities, nativeIdentityLocator{Stream: stream, Digest: row.Native, Ref: row.Cursor})
			keys = append(keys, row.Native)
		}
	}
	if len(identities) == 0 {
		return nil
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(identities, indexWriteBatch).Error; err != nil {
		return err
	}
	if err := db.Where("stream = ? AND digest IN ?", stream, keys).Find(&identities).Error; err != nil {
		return err
	}
	for _, row := range identities {
		refs[string(row.Digest)] = row.Ref
	}
	for j := range rows {
		rows[j].NativeRef = refs[string(rows[j].Native)]
	}
	return internNativeMessages(db, stream, rows)
}

type nativeMessageIdentity struct {
	// invocation owns the message.
	invocation int64
	// message is the original stable native message ID.
	message string
}

func internNativeMessages(db *gorm.DB, stream int64, rows []rowIndex) error {
	var lanes []nativeMessageLocator
	refs := make(map[nativeMessageIdentity]int64)
	for _, row := range rows {
		if row.NativeRef == 0 || row.Message == "" {
			continue
		}
		key := nativeMessageIdentity{row.NativeRef, row.Message}
		if refs[key] == 0 {
			refs[key] = row.Cursor
			lanes = append(lanes, nativeMessageLocator{Stream: stream, Identity: row.NativeRef, Message: indexIdentity(row.Message), Ref: row.Cursor})
		}
	}
	if len(lanes) == 0 {
		return nil
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(lanes, indexWriteBatch).Error; err != nil {
		return err
	}
	const batch = 64
	for start := 0; start < len(lanes); start += batch {
		end := min(start+batch, len(lanes))
		var query strings.Builder
		args := make([]any, 0, (end-start)*3)
		for j, lane := range lanes[start:end] {
			if j > 0 {
				query.WriteString(" UNION ALL ")
			}
			query.WriteString("SELECT * FROM conversation_native_messages WHERE stream = ? AND identity = ? AND message = ?")
			args = append(args, stream, lane.Identity, lane.Message)
		}
		var found []nativeMessageLocator
		if err := db.Raw(query.String(), args...).Scan(&found).Error; err != nil {
			return err
		}
		for _, lane := range found {
			refs[nativeMessageIdentity{lane.Identity, string(lane.Message)}] = lane.Ref
		}
	}
	for j := range rows {
		rows[j].LaneRef = refs[nativeMessageIdentity{rows[j].NativeRef, rows[j].Message}]
	}
	return nil
}
