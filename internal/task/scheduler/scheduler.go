/*
Copyright 2025 linux.do

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"fmt"
	"sync"
	"time"

	"github.com/linux-do/credit/internal/config"
	"github.com/linux-do/credit/internal/task"

	"github.com/hibiken/asynq"
)

var (
	AsynqClient   *asynq.Client
	scheduler     *asynq.Scheduler
	schedulerOnce sync.Once
)

func init() {
	AsynqClient = asynq.NewClient(task.RedisOpt)
}

// StartScheduler 启动调度器
func StartScheduler() error {
	var err error
	schedulerOnce.Do(func() {
		location, locErr := time.LoadLocation("Asia/Shanghai")
		if locErr != nil {
			err = fmt.Errorf("failed to load location: %w", locErr)
			return
		}
		scheduler = asynq.NewScheduler(
			task.RedisOpt,
			&asynq.SchedulerOpts{
				Location: location,
			},
		)

		if err = registerSchedules(scheduler, config.Config.Features); err != nil {
			return
		}

		err = scheduler.Run()
	})
	return err
}

type scheduleDefinition struct {
	cron     string
	taskType string
	queue    string
	maxRetry int
	unique   time.Duration
}

// ScheduledTaskTypes reports the task types that StartScheduler will register.
func ScheduledTaskTypes(flags config.Features) []string {
	definitions := scheduleDefinitions(flags)
	taskTypes := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		taskTypes = append(taskTypes, definition.taskType)
	}
	return taskTypes
}

func registerSchedules(s *asynq.Scheduler, flags config.Features) error {
	for _, definition := range scheduleDefinitions(flags) {
		options := []asynq.Option{asynq.Unique(definition.unique)}
		if definition.maxRetry > 0 {
			options = append(options, asynq.MaxRetry(definition.maxRetry))
		}
		if definition.queue != "" {
			options = append(options, asynq.Queue(definition.queue))
		}
		if _, err := s.Register(definition.cron, asynq.NewTask(definition.taskType, nil), options...); err != nil {
			return err
		}
	}
	return nil
}

func scheduleDefinitions(flags config.Features) []scheduleDefinition {
	definitions := []scheduleDefinition{
		{config.Config.Scheduler.CleanupUnusedUploadsTaskCron, task.CleanupUnusedUploadsTask, "", 3, 23 * time.Hour},
	}
	if flags.LegacyGamificationImport {
		definitions = append(definitions,
			scheduleDefinition{config.Config.Scheduler.UpdateUserGamificationScoresTaskCron, task.UpdateUserGamificationScoresTask, task.QueueWhitelistOnly, 5, 23 * time.Hour},
		)
	}
	if flags.LegacyCommerce {
		definitions = append(definitions,
			scheduleDefinition{config.Config.Scheduler.AutoRefundExpiredDisputesTaskCron, task.AutoRefundExpiredDisputesTask, "", 5, 23 * time.Hour},
			scheduleDefinition{config.Config.Scheduler.SyncOrdersToClickHouseTaskCron, task.SyncOrdersToClickHouseTask, "", 10, 23 * time.Hour},
			scheduleDefinition{config.Config.Scheduler.RefundExpiredRedEnvelopesTaskCron, task.RefundExpiredRedEnvelopesTask, "", 0, 23 * time.Hour},
			scheduleDefinition{config.Config.Scheduler.SettlePendingPaymentsTaskCron, task.SettlePendingPaymentsTask, "", 3, 55 * time.Minute},
		)
	}
	return definitions
}
