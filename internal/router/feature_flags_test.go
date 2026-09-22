package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/linux-do/credit/internal/config"
	"github.com/linux-do/credit/internal/task"
	"github.com/linux-do/credit/internal/task/scheduler"
	"github.com/linux-do/credit/internal/task/worker"
)

var legacyCommercePaths = []string{
	"/pay/submit.php",
	"/api/v1/payment/transfer",
	"/api/v1/redenvelope/create",
	"/api/v1/merchant/api-keys",
}

func routerForFeatures(t *testing.T, flags config.Features) *gin.Engine {
	t.Helper()

	previousPrefix := config.Config.App.APIPrefix
	config.Config.App.APIPrefix = "/api"
	t.Cleanup(func() { config.Config.App.APIPrefix = previousPrefix })

	r := gin.New()
	registerRoutes(r, flags)
	return r
}

func TestCommerceRoutesAbsentByDefault(t *testing.T) {
	r := routerForFeatures(t, config.Features{})

	for _, path := range legacyCommercePaths {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d, want %d", path, w.Code, http.StatusNotFound)
		}
	}
}

func TestCommerceRoutesReturnWhenLegacyCommerceEnabled(t *testing.T) {
	r := routerForFeatures(t, config.Features{LegacyCommerce: true})

	for _, path := range legacyCommercePaths {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code == http.StatusNotFound {
			t.Fatalf("%s: route is missing when legacy commerce is enabled", path)
		}
	}
}

func TestSharedRoutesRemainWhenCommerceDisabled(t *testing.T) {
	r := routerForFeatures(t, config.Features{})
	routes := make(map[string]bool, len(r.Routes()))
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{"GET /f/:id", "GET /api/v1/oauth/login"} {
		if !routes[route] {
			t.Fatalf("%s: shared route is missing when commerce is disabled", route)
		}
	}
}

func TestLegacyTasksAbsentByDefault(t *testing.T) {
	flags := config.Features{}
	mux := worker.NewServeMux(flags)
	for _, taskType := range []string{
		task.UpdateUserGamificationScoresTask,
		task.UpdateSingleUserGamificationScoreTask,
		task.AutoRefundExpiredDisputesTask,
		task.AutoRefundSingleDisputeTask,
		task.MerchantPaymentNotifyTask,
		task.SyncOrdersToClickHouseTask,
		task.RefundExpiredRedEnvelopesTask,
		task.SettlePendingPaymentsTask,
	} {
		_, pattern := mux.Handler(asynq.NewTask(taskType, nil))
		if pattern != "" {
			t.Fatalf("worker registered legacy task %q as %q", taskType, pattern)
		}
	}

	legacyScheduled := map[string]bool{
		task.UpdateUserGamificationScoresTask: true,
		task.AutoRefundExpiredDisputesTask:    true,
		task.SyncOrdersToClickHouseTask:       true,
		task.RefundExpiredRedEnvelopesTask:    true,
		task.SettlePendingPaymentsTask:        true,
	}
	for _, taskType := range scheduler.ScheduledTaskTypes(flags) {
		if legacyScheduled[taskType] {
			t.Fatalf("scheduler registered legacy task %q", taskType)
		}
	}

	legacyDispatchable := map[string]bool{
		task.TaskTypeOrderSync:         true,
		task.TaskTypeUserGamification:  true,
		task.TaskTypeDisputeRefund:     true,
		task.TaskTypeRedEnvelopeRefund: true,
		task.TaskTypeSettlePending:     true,
	}
	for _, taskMeta := range task.DispatchableTasksForFeatures(flags) {
		if legacyDispatchable[taskMeta.Type] {
			t.Fatalf("admin dispatch exposes legacy task %q", taskMeta.Type)
		}
	}
}
