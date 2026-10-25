package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// vecTableName 是 sqlite-vec 向量表名。
const vecTableName = "memory_vectors"

// vectorStore 是 sqlite-vec 向量存取实现。
type vectorStore struct {
	// store 是共享的记忆存储层。
	store *store
}

// serializeVector 把 float32 向量序列化为 sqlite-vec 期望的紧凑小端 BLOB。
func serializeVector(values []float32) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.LittleEndian, values)
	return buffer.Bytes()
}

// queryRower 抽象 *sql.DB 与 *sql.Tx 的单行查询，事务内查询必须使用事务连接，
// 否则在单连接池上限下会永久等待空闲连接。
type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

// vecTableExists 返回向量表是否已创建；事务内调用必须传入事务连接。
func (v *vectorStore) vecTableExists(queryer queryRower) (bool, error) {
	var name string
	err := queryer.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
		vecTableName,
	).Scan(&name)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, status_error.DatabaseError
}

// ensureVecTable 在事务内按指定维度创建向量表（若尚不存在）。
func (v *vectorStore) ensureVecTable(tx *sql.Tx, dimensions int) error {
	exists, err := v.vecTableExists(tx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	statement := `CREATE VIRTUAL TABLE memory_vectors USING vec0(
		memory_id TEXT PRIMARY KEY,
		embedding FLOAT[` + strconv.Itoa(dimensions) + `] DISTANCE_METRIC=cosine,
		scope TEXT,
		scope_id TEXT
	)`
	if _, err := tx.Exec(statement); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// recreateVecTable 在事务内删除并按新维度重建向量表，用于 reindex 原子切换。
func (v *vectorStore) recreateVecTable(tx *sql.Tx, dimensions int) error {
	if err := v.dropVecTable(tx); err != nil {
		return err
	}
	return v.ensureVecTable(tx, dimensions)
}

// dropVecTable 删除现有向量表；表不存在时不动作。
func (v *vectorStore) dropVecTable(tx *sql.Tx) error {
	exists, err := v.vecTableExists(tx)
	if err != nil || !exists {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE memory_vectors`); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// insertVector 在事务内写入一条向量，scope 作为分区键随向量保存。
func (v *vectorStore) insertVector(
	tx *sql.Tx,
	memoryID string,
	vector []float32,
	scope constants.MemoryScope,
	scopeID string,
) error {
	_, err := tx.Exec(
		`INSERT INTO memory_vectors (memory_id, embedding, scope, scope_id) VALUES (?, ?, ?, ?)`,
		memoryID,
		serializeVector(vector),
		string(scope),
		scopeID,
	)
	if err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteVector 在事务内删除一条向量；向量表不存在时不动作。
func (v *vectorStore) deleteVector(tx *sql.Tx, memoryID string) error {
	exists, err := v.vecTableExists(tx)
	if err != nil || !exists {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM memory_vectors WHERE memory_id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteScopeVectors 在事务内删除指定范围的全部向量；向量表不存在时不动作。
func (v *vectorStore) deleteScopeVectors(
	tx *sql.Tx,
	scope constants.MemoryScope,
	scopeID string,
) error {
	exists, err := v.vecTableExists(tx)
	if err != nil || !exists {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM memory_vectors WHERE scope = ? AND scope_id = ?`,
		string(scope),
		scopeID,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// vectorCount 返回语义索引中的向量条数；向量表不存在时为 0。
func (v *vectorStore) vectorCount() (int64, error) {
	exists, err := v.vecTableExists(v.store.db)
	if err != nil || !exists {
		return 0, err
	}
	var count int64
	if err := v.store.db.QueryRow(`SELECT count(*) FROM memory_vectors`).Scan(&count); err != nil {
		return 0, status_error.DatabaseError
	}
	return count, nil
}

// search 执行 sqlite-vec 语义 KNN，并把 cosine distance 转换为相似度。
// 此处为检索特例：虚拟表负责排名，memories 负责范围与正文，单次查询保持排名、范围和版本一致。
func (v *vectorStore) search(
	ctx context.Context,
	vector []float32,
	topK int,
	scope constants.MemoryScope,
	scopeID string,
) ([]ScoredMemory, error) {
	exists, err := v.vecTableExists(v.store.db)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []ScoredMemory{}, nil
	}
	rows, err := v.store.db.QueryContext(
		ctx,
		`SELECT `+memoryColumnsAliased+`, v.distance
		 FROM memory_vectors v
		 JOIN memories m ON m.id = v.memory_id
		 WHERE v.embedding MATCH ? AND k = ? AND v.scope = ? AND v.scope_id = ?
		 ORDER BY v.distance ASC`,
		serializeVector(vector),
		topK,
		string(scope),
		scopeID,
	)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	results := []ScoredMemory{}
	for rows.Next() {
		memory := Memory{}
		var distance float64
		if err := rows.Scan(
			&memory.Seq,
			&memory.ID,
			&memory.Scope,
			&memory.ScopeID,
			&memory.Data,
			&memory.Hash,
			&memory.SourceRunID,
			&memory.CreatedAt,
			&memory.UpdatedAt,
			&distance,
		); err != nil {
			return nil, status_error.DatabaseError
		}
		results = append(results, ScoredMemory{
			Memory: memory,
			Score:  cosineSimilarity(distance),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, status_error.DatabaseError
	}
	return results, nil
}

// cosineSimilarity 把 cosine distance 转换为 [0,1] 相似度，越大越相似。
func cosineSimilarity(distance float64) float64 {
	score := 1.0 - distance
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
