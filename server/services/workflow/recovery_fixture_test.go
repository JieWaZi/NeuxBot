package workflow

import (
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func openRecoveryWorkflow(t *testing.T, databasePath string) (*Service, func()) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: databasePath, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	close := func() {
		once.Do(func() {
			if err := connection.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(close)
	return workflowTestNew(Config{Store: controllers.NewWorkflowController(database.DB), Runs: controllers.NewWorkflowRunController(database.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}), close
}
