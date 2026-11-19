package im

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	imservice "github.com/JieWaZi/NeuxBot/server/services/im"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestUpdateIMBindingSavesSettingsAndChangesAuthorizationIndependently(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "im.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := controllers.NewIMController(db.DB)
	previousStore, previousIM, previousTeams := global.IMControllerManager, global.IMService, global.ChannelControllerManager
	global.IMControllerManager = store
	global.IMService = imservice.New(imservice.Config{
		Store: store, Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
	})
	global.ChannelControllerManager = controllers.NewChannelController(db.DB)
	t.Cleanup(func() {
		global.IMControllerManager, global.IMService, global.ChannelControllerManager = previousStore, previousIM, previousTeams
	})
	if err := db.DB.Create(&models.Channel{ID: "team", Name: "研发团队", LeadWakerID: "lead"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	saved := models.IMBinding{
		ID: "binding", ConnectionID: "connection", ExternalChatID: "group", Kind: constants.IMChatKindGroup,
		Name: "团体聊天", State: constants.IMBindingStateApproved, ChannelID: "team", ActiveLinkID: "active", ControllerIDs: []string{"first"},
	}
	if err := store.DiscoverBinding(ctx, saved); err != nil {
		t.Fatal(err)
	}
	first := &BindingConfiguration{Name: "先保存的设置", ChannelID: "team", ControllerIDs: []string{"first"}}
	last := &BindingConfiguration{Name: "后保存的设置", ChannelID: "team", ControllerIDs: []string{"last"}, AllowWorkflowDelivery: true}
	for _, config := range []*BindingConfiguration{first, last} {
		call := UpdateIMBinding{BindingID: saved.ID, Body: UpdateIMBindingRequest{State: constants.IMBindingStateApproved, Configuration: config}}
		if _, err := call.Output(ctx); err != nil {
			t.Fatalf("save configuration %q: %v", config.Name, err)
		}
	}
	for _, state := range []constants.IMBindingState{constants.IMBindingStateRejected, constants.IMBindingStateApproved} {
		call := UpdateIMBinding{BindingID: saved.ID, Body: UpdateIMBindingRequest{State: state}}
		if _, err := call.Output(ctx); err != nil {
			t.Fatalf("set authorization %s: %v", state, err)
		}
		actual, err := store.Binding(ctx, saved.ID)
		if err != nil || actual.State != state || actual.Name != last.Name || actual.ChannelID != last.ChannelID ||
			!reflect.DeepEqual(actual.ControllerIDs, last.ControllerIDs) || !actual.AllowWorkflowDelivery || actual.ActiveLinkID != saved.ActiveLinkID {
			t.Fatalf("unexpected settings after authorization %s: %+v %v", state, actual, err)
		}
		if state == constants.IMBindingStateRejected {
			last.Name = "撤销后保存的设置"
			save := UpdateIMBinding{BindingID: saved.ID, Body: UpdateIMBindingRequest{Configuration: last}}
			if _, err := save.Output(ctx); err != nil {
				t.Fatalf("save settings without changing authorization: %v", err)
			}
			actual, err := store.Binding(ctx, saved.ID)
			if err != nil || actual.State != state || actual.Name != last.Name {
				t.Fatalf("saving settings changed authorization: %+v %v", actual, err)
			}
		}
	}
	last.WorkingDirectory = t.TempDir()
	saveWorkingDirectory := UpdateIMBinding{BindingID: saved.ID, Body: UpdateIMBindingRequest{Configuration: last}}
	if _, err := saveWorkingDirectory.Output(ctx); err != nil {
		t.Fatal(err)
	}
	revoke := UpdateIMBinding{BindingID: saved.ID, Body: UpdateIMBindingRequest{State: constants.IMBindingStateRejected}}
	if _, err := revoke.Output(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(last.WorkingDirectory); err != nil {
		t.Fatal(err)
	}
	approve := UpdateIMBinding{BindingID: saved.ID, Body: UpdateIMBindingRequest{State: constants.IMBindingStateApproved}}
	if _, err := approve.Output(ctx); !errors.Is(err, status_error.IMConfigurationInvalid) {
		t.Fatalf("approval accepted a removed working directory: %v", err)
	}
	if current, err := store.Binding(ctx, saved.ID); err != nil || current.State != constants.IMBindingStateRejected || current.WorkingDirectory != last.WorkingDirectory {
		t.Fatalf("failed approval changed the saved binding: %+v %v", current, err)
	}
}
