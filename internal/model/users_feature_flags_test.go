package model

import (
	"context"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/linux-do/credit/internal/config"
	"github.com/linux-do/credit/internal/task/scheduler"
)

func unreachableAsynqClient() *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
}

func TestEnqueueBadgeScoreTaskSkipsQueueWhenLegacyImportDisabled(t *testing.T) {
	previousFeatures := config.Config.Features
	previousClient := scheduler.AsynqClient
	client := unreachableAsynqClient()
	config.Config.Features = config.Features{}
	scheduler.AsynqClient = client
	t.Cleanup(func() {
		config.Config.Features = previousFeatures
		scheduler.AsynqClient = previousClient
		_ = client.Close()
	})

	err := (&User{ID: 42, Username: "disabled"}).EnqueueBadgeScoreTask(context.Background(), 0)
	if err != nil {
		t.Fatalf("disabled legacy import must not enqueue a score task: %v", err)
	}
}

func TestEnqueueBadgeScoreTaskAttemptsQueueWhenLegacyImportEnabled(t *testing.T) {
	previousFeatures := config.Config.Features
	previousClient := scheduler.AsynqClient
	client := unreachableAsynqClient()
	config.Config.Features = config.Features{LegacyGamificationImport: true}
	scheduler.AsynqClient = client
	t.Cleanup(func() {
		config.Config.Features = previousFeatures
		scheduler.AsynqClient = previousClient
		_ = client.Close()
	})

	err := (&User{ID: 42, Username: "enabled"}).EnqueueBadgeScoreTask(context.Background(), 0)
	if err == nil {
		t.Fatal("enabled legacy import must attempt to enqueue a score task")
	}
}
