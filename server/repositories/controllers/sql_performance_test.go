package controllers

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlPerformanceLog 只累计语句次数；正式计时不构造 SQL 文本。
type sqlPerformanceLog struct {
	// Interface 复用其他日志接口。
	logger.Interface
	// Queries 是当前测量窗口的 SQL 次数。
	Queries int64
	// Inspect 在预热时额外读取结果行数，不计入耗时和分配。
	Inspect bool
	// MaxRows 是单条 SQL 返回应用的最大行数，不代表数据库内部扫描量。
	MaxRows int64
}

// LogMode 保留计数器，仓储关闭文本日志时仍可观测实际查询。
func (l *sqlPerformanceLog) LogMode(logger.LogLevel) logger.Interface { return l }

func (l *sqlPerformanceLog) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	l.Queries++
	if l.Inspect {
		_, rows := fc()
		l.MaxRows = max(l.MaxRows, rows)
	}
}

// seedSQLPerformance 用模型生成完整合法列，只用集合插入加速造数；造数不计入测量。
func seedSQLPerformance(b *testing.B, db *gorm.DB, prototype any, count int, expressions map[string]string) {
	b.Helper()
	statement := db.Session(&gorm.Session{DryRun: true, SkipDefaultTransaction: true}).Create(prototype)
	if statement.Error != nil {
		b.Fatal(statement.Error)
	}
	query := statement.Statement.SQL.String()
	end := strings.Index(query, ") VALUES")
	start := strings.Index(query, "(")
	if start < 0 || end < 0 {
		b.Fatalf("unsupported fixture insert: %s", query)
	}
	columns := strings.Split(query[start+1:end], ",")
	selects, args := []string{}, []any{count, count}
	if len(columns) != len(statement.Statement.Vars) {
		b.Fatal("fixture columns/arguments mismatch")
	}
	for i, col := range columns {
		name := strings.Trim(col, "`\" ")
		if expression, ok := expressions[name]; ok {
			selects = append(selects, expression)
		} else {
			selects = append(selects, "?")
			args = append(args, statement.Statement.Vars[i])
		}
	}
	query = "WITH RECURSIVE fixture(n) AS (SELECT 0 WHERE ?>0 UNION ALL SELECT n+1 FROM fixture WHERE n+1<?) " + query[:end+1] + " SELECT " + strings.Join(selects, ",") + " FROM fixture"
	if err := db.Exec(query, args...).Error; err != nil {
		b.Fatal(err)
	}
}

// benchmarkSQLRead 同时报告端到端仓储耗时、分配和 SQL 次数；每例先验证一次结果。
func benchmarkSQLRead(b *testing.B, db *gorm.DB, read func(context.Context, *gorm.DB) error) {
	b.Helper()
	log := &sqlPerformanceLog{Interface: logger.Discard, Inspect: true}
	measured := db.Session(&gorm.Session{Logger: log})
	if err := read(context.Background(), measured); err != nil {
		b.Fatal(err)
	}
	if log.Queries == 0 {
		b.Fatal("SQL observer did not capture the measured read")
	}
	log.Inspect, log.Queries = false, 0
	samples := make([]float64, b.N)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		err := read(context.Background(), measured)
		samples[i] = float64(time.Since(started).Nanoseconds()) / 1e6
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	sort.Float64s(samples)
	b.ReportMetric(samples[(len(samples)-1)/2], "p50-ms/op")
	b.ReportMetric(samples[int(math.Ceil(float64(len(samples))*.95))-1], "p95-ms/op")
	b.ReportMetric(float64(log.Queries)/float64(b.N), "SQL/op")
	b.ReportMetric(float64(log.MaxRows), "max-rows/SQL")
}

// sqlPerformanceDataset 记录精确的造数规模，历史、活跃请求与当前失败分别增长。
type sqlPerformanceDataset struct {
	// Name 是基准子场景名称。
	Name string
	// History 是无待办聊天及旧自动运行的各自数量。
	History int
	// Pending 是等待审批的聊天数量。
	Pending int
	// Results 是有未读成果的聊天数量。
	Results int
	// Failures 是各有一次当前失败的自动任务数量。
	Failures int
}

func sqlPerformanceDatabase(b *testing.B, data sqlPerformanceDataset) *gorm.DB {
	b.Helper()
	db := newTestDB(b).Session(&gorm.Session{Logger: logger.Discard})
	if err := db.Create(&models.Waker{ID: "waker", Name: "性能伙伴"}).Error; err != nil {
		b.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		seedSQLPerformance(b, tx, &models.Chat{WakerID: "waker", Title: "普通历史", AdditionalWorkingDirectories: []string{}}, data.History, map[string]string{"id": "'h'||printf('%031d',n)", "title": "CASE WHEN n%997=0 THEN '稀疏目标' ELSE '普通历史' END", "updated_at": "n+1"})
		seedSQLPerformance(b, tx, &models.Chat{WakerID: "waker", Title: "待处理", ActiveExecutionID: "execution", WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: "request", Title: strings.Repeat("审批内容", 64), RequestedAt: 10}}}, data.Pending, map[string]string{"id": "'p'||printf('%031d',n)", "updated_at": "n+1000000", "pending_approvals": "json_array(json_object('approvalId','request-'||n,'title','审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容审批内容','requestedAt',10))"})
		seedSQLPerformance(b, tx, &models.Chat{WakerID: "waker", Title: "未读成果", LatestReadableSequence: 1}, data.Results, map[string]string{"id": "'r'||printf('%031d',n)", "updated_at": "n+2000000"})
		if err := tx.Exec("INSERT INTO chat_read_cursors(reader_id,chat_id,updated_at) SELECT ?,id,1 FROM chats", constants.LocalReaderID).Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO chat_readable_messages(chat_id,message_id,order_sequence,kind,created_at) SELECT id,'answer',1,'answer',1 FROM chats WHERE latest_readable_sequence=1").Error; err != nil {
			return err
		}
		seedSQLPerformance(b, tx, &models.AutomaticTask{WakerID: "waker", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{Name: "自动失败", Instruction: strings.Repeat("失败工作", 256)}}, data.Failures, map[string]string{"id": "'t'||printf('%031d',n)"})
		seedSQLPerformance(b, tx, &models.AutomaticRun{WakerID: "waker", Admission: constants.AutomaticRunAdmissionFailed, Configuration: models.AutomaticTaskConfiguration{Name: "自动失败", Instruction: strings.Repeat("冻结指令", 256)}, CreatedAt: 1, UpdatedAt: 3000000}, data.Failures, map[string]string{"id": "'f'||printf('%031d',n)", "task_id": "'t'||printf('%031d',n)", "updated_at": "n+3000000"})
		if data.Failures > 0 {
			seedSQLPerformance(b, tx, &models.AutomaticRun{WakerID: "waker", Admission: constants.AutomaticRunAdmissionAccepted, Configuration: models.AutomaticTaskConfiguration{Name: "历史运行"}, CreatedAt: -1, UpdatedAt: 1}, data.History, map[string]string{"id": "'a'||printf('%031d',n)", "task_id": fmt.Sprintf("'t'||printf('%%031d',n%%%d)", data.Failures), "occurrence_key": "'history-'||n"})
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	return db
}

// BenchmarkSQLRead 使用生产数据库装配和默认 SQLite 模式，顺序测量，禁止并发造数干扰读延迟。
func BenchmarkSQLRead(b *testing.B) {
	for _, data := range []sqlPerformanceDataset{
		{Name: "History1k", History: 1000, Pending: 100, Results: 100, Failures: 100},
		{Name: "History10k", History: 10000, Pending: 100, Results: 100, Failures: 100},
		{Name: "History100k", History: 100000, Pending: 100, Results: 100, Failures: 100},
		{Name: "Pending1k", History: 10000, Pending: 1000, Results: 100, Failures: 100},
		{Name: "Pending10k", History: 10000, Pending: 10000, Results: 100, Failures: 100},
		{Name: "Unread1k", History: 10000, Pending: 100, Results: 1000, Failures: 1000},
		{Name: "Unread10k", History: 10000, Pending: 100, Results: 10000, Failures: 10000},
	} {
		b.Run(data.Name, func(b *testing.B) {
			db := sqlPerformanceDatabase(b, data)
			cases := []struct {
				// Name 是用户读取场景。
				Name string
				// Read 执行并验证一次读取。
				Read func(context.Context, *gorm.DB) error
			}{
				{"BoardSummary", func(ctx context.Context, db *gorm.DB) error {
					return NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
						board := NewWorkBoardController(db)
						q := WorkBoardQuery{Limit: 5}
						counts, err := board.Counts(ctx, q)
						if err != nil {
							return err
						}
						actions, actionCount, actionWorks, err := board.Attention(ctx, q)
						if err != nil {
							return err
						}
						reading, _, err := board.ReadingMetrics(ctx, constants.LocalReaderID, q)
						if err != nil {
							return err
						}
						if counts.Total != int64(data.History+data.Pending+data.Results) || actionWorks != int64(data.Pending) || actionCount != int64(data.Pending) || len(actions) != 6 || reading.AwaitingViewWorkCount != int64(data.Results) {
							b.Fatalf("incorrect board summary: %+v %d %d", counts, actionWorks, actionCount)
						}
						return nil
					})
				}},
				{"BoardFirst", func(ctx context.Context, db *gorm.DB) error {
					return NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
						board := NewWorkBoardController(db)
						q := WorkBoardQuery{Limit: 50}
						count, err := board.Count(ctx, q)
						if err != nil {
							return err
						}
						page, err := board.List(ctx, q)
						if err == nil && (count != int64(data.History+data.Pending+data.Results) || len(page) != 51) {
							b.Fatalf("incorrect board: %d %d", len(page), count)
						}
						return err
					})
				}},
				{"BoardDeep", func(ctx context.Context, db *gorm.DB) error {
					return NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
						board := NewWorkBoardController(db)
						q := WorkBoardQuery{Limit: 50, Offset: data.History + data.Pending + data.Results - 50}
						count, err := board.Count(ctx, q)
						if err != nil {
							return err
						}
						page, err := board.Page(ctx, q)
						if err == nil && (count != int64(data.History+data.Pending+data.Results) || len(page) != 50) {
							b.Fatalf("incorrect board deep: %d %d", len(page), count)
						}
						return err
					})
				}},
				{"SparseTitle", func(ctx context.Context, db *gorm.DB) error {
					return NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
						board := NewWorkBoardController(db)
						q := WorkBoardQuery{Limit: 50, Query: "稀疏目标"}
						count, err := board.Count(ctx, q)
						if err != nil {
							return err
						}
						page, err := board.List(ctx, q)
						if err == nil && (count != int64((data.History+996)/997) || len(page) != min(51, int(count))) {
							b.Fatalf("incorrect search: %d %d", len(page), count)
						}
						return err
					})
				}},
			}
			for _, scenario := range cases {
				b.Run(scenario.Name, func(b *testing.B) { benchmarkSQLRead(b, db, scenario.Read) })
			}
		})
	}
}

// BenchmarkSQLIMDependencies 使用同一共享子版本，分别增加父调用数，检查宽依赖图的查询次数。
func BenchmarkSQLIMDependencies(b *testing.B) {
	for _, width := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("Children%d", width), func(b *testing.B) {
			db := newTestDB(b).Session(&gorm.Session{Logger: logger.Discard})
			meta := `export const meta={sdk:'ally.workflow',title:'调用',description:'子流程',phases:[{id:'phase',title:'调用',detail:'调用',canExit:true}]};`
			leafSource := `export const meta={sdk:'ally.workflow',title:'发送',description:'发送',phases:[{id:'phase',title:'发送',detail:'发送',canExit:true}],actions:[{id:'send',label:'发送',description:'发送',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012'],imTargetIds:['target']}]};const actions={send:async(ctx,input)=>null};return await action('send',{id:'deliver',label:'发送',phase:'phase'});`
			leaf, _, err := workflowdsl.Compile(leafSource)
			if err != nil {
				b.Fatal(err)
			}
			var body strings.Builder
			for i := 0; i < width; i++ {
				fmt.Fprintf(&body, "await workflow('leaf',{id:'call-%04d',label:'子流程',phase:'phase'});", i)
			}
			parent, diagnostics, err := workflowdsl.Compile(meta + body.String() + "return null;")
			if err != nil {
				b.Fatalf("%v %+v", err, diagnostics)
			}
			for _, value := range []any{&models.IMConnection{ID: "connection"}, &models.IMBinding{ID: "target", ConnectionID: "connection"}, &models.WorkflowDefinition{ID: "parent", CurrentRevisionID: "parent-version"}, &models.WorkflowDefinition{ID: "leaf", CurrentRevisionID: "leaf-version"}, &models.WorkflowRevision{ID: "parent-version", DefinitionID: "parent", Manifest: parent.Manifest, Bundle: parent.Bundle}, &models.WorkflowRevision{ID: "leaf-version", DefinitionID: "leaf", Manifest: leaf.Manifest, Bundle: leaf.Bundle}} {
				if err := db.Create(value).Error; err != nil {
					b.Fatal(err)
				}
			}
			seedSQLPerformance(b, db, &models.WorkflowRevision{DefinitionID: "leaf", Source: strings.Repeat("历史源码", 512), Manifest: leaf.Manifest, Bundle: leaf.Bundle}, 10000, map[string]string{"id": "'v'||printf('%031d',n)", "ordinal": "n+2"})
			benchmarkSQLRead(b, db, func(ctx context.Context, db *gorm.DB) error {
				var value models.IMConnectionDependencies
				err := NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
					var err error
					value, err = NewIMController(db).ConnectionDependencies(ctx, "connection")
					return err
				})
				if err == nil && len(value.Workflows) != width+1 {
					b.Fatalf("incorrect dependencies: %d", len(value.Workflows))
				}
				return err
			})
		})
	}
}

// TestSQLPendingBatchPlan 覆盖真实主键续批的排序，避免只检查无排序查询而遗漏重复排序。
func TestSQLPendingBatchPlan(t *testing.T) {
	db := newTestDB(t)
	log := &repositoryQueryLog{Interface: logger.Discard}
	measured := db.Session(&gorm.Session{Logger: log})
	if err := NewTransactionController(dbhelper.NewGormTransactionManager(measured)).Transaction(context.Background(), func(ctx context.Context) error {
		return (&workBoardController{db: measured}).walkAttention(ctx, time.Now().UnixMilli(), boardAttentionScope{}, false, func(models.WorkBoardAttention) error { return nil })
	}); err != nil {
		t.Fatal(err)
	}
	if len(log.Statements) == 0 {
		t.Fatal("missing production pending query")
	}
	query := log.Statements[0]
	orderAt := strings.Index(query, " ORDER BY")
	if orderAt < 0 {
		t.Fatalf("pending query lost primary-key ordering: %s", query)
	}
	for _, candidate := range []string{query, query[:orderAt] + " AND chats.id>'p000'" + query[orderAt:]} {
		var steps []struct {
			// Detail 是 SQLite 查询计划的一步。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN " + candidate).Scan(&steps).Error; err != nil {
			t.Fatal(err)
		}
		details := []string{}
		for _, step := range steps {
			details = append(details, step.Detail)
		}
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, "idx_chat_pending_id") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatalf("pending batches must seek the ordered active index: %s", plan)
		}
		if candidate != query && !strings.Contains(plan, "SEARCH chats") {
			t.Fatalf("continuation must seek the primary key: %s", plan)
		}
		t.Log(plan)
	}
}

// BenchmarkSQLBoardOwners 单独覆盖团队与工作流历史，防止仅有聊天数据掩盖其他分支成本。
func BenchmarkSQLBoardOwners(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("Each%d", count), func(b *testing.B) {
			db := newTestDB(b).Session(&gorm.Session{Logger: logger.Discard})
			if err := db.Create(&models.WorkflowDefinition{ID: "workflow", Title: "工作流"}).Error; err != nil {
				b.Fatal(err)
			}
			if err := db.Create(&models.Channel{ID: "team", Name: "团队"}).Error; err != nil {
				b.Fatal(err)
			}
			if err := db.Create(&models.Waker{ID: "waker", Name: "团队成员"}).Error; err != nil {
				b.Fatal(err)
			}
			ready := int64(1)
			if err := db.Transaction(func(tx *gorm.DB) error {
				seedSQLPerformance(b, tx, &models.ChannelThread{ChannelID: "team", Title: "团队工作", ReadyAt: &ready, AdditionalWorkingDirectories: []string{}}, count, map[string]string{"id": "'t'||printf('%031d',n)", "lead_chat_id": "'l'||printf('%031d',n)", "updated_at": "n+1000000"})
				seedSQLPerformance(b, tx, &models.Chat{WakerID: "waker", Title: "团队成员", HiddenFromWakerHistory: true, AdditionalWorkingDirectories: []string{}}, count, map[string]string{"id": "'l'||printf('%031d',n)"})
				seedSQLPerformance(b, tx, &models.ChannelThreadMember{WakerID: "waker", DisplayName: "团队成员"}, count, map[string]string{"thread_id": "'t'||printf('%031d',n)", "chat_id": "'l'||printf('%031d',n)"})
				seedSQLPerformance(b, tx, &models.WorkflowRun{DefinitionID: "workflow", Status: constants.WorkflowRunStatusSucceeded}, count, map[string]string{"id": "'w'||printf('%031d',n)", "updated_at": "n+2000000"})
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			for _, mode := range []string{"First", "Deep"} {
				b.Run(mode, func(b *testing.B) {
					benchmarkSQLRead(b, db, func(ctx context.Context, db *gorm.DB) error {
						return NewTransactionController(dbhelper.NewGormTransactionManager(db)).Transaction(ctx, func(ctx context.Context) error {
							board := NewWorkBoardController(db)
							q := WorkBoardQuery{Limit: 50}
							total, err := board.Count(ctx, q)
							var page []models.WorkBoardEntry
							if err != nil {
								return err
							}
							if mode == "Deep" {
								q.Offset = 2*count - 50
								page, err = board.Page(ctx, q)
							} else {
								page, err = board.List(ctx, q)
							}
							wantRows := 51
							if mode == "Deep" {
								wantRows = 50
							}
							if err == nil && (total != int64(2*count) || len(page) != wantRows) {
								b.Fatalf("incorrect mixed owners: %d %d", len(page), total)
							}
							return err
						})
					})
				})
			}
		})
	}
}
