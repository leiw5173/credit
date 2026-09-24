/*
Copyright 2026 linux.do

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

package model

import "time"

// LedgerAccount is the integer-unit projection for a forum user.
type LedgerAccount struct {
	ID                int64     `json:"id"`
	ForumUserID       int64     `json:"forum_user_id"`
	AvailableBalance  int64     `json:"available_balance"`
	FrozenBalance     int64     `json:"frozen_balance"`
	PendingBalance    int64     `json:"pending_balance"`
	ProjectionVersion int64     `json:"projection_version"`
	CreatedAt         time.Time `json:"created_at"`
}

// LedgerEntry is an immutable, integer-unit accounting entry.
type LedgerEntry struct {
	ID                int64     `json:"id"`
	AccountID         int64     `json:"account_id"`
	Action            string    `json:"action"`
	AvailableDelta    int64     `json:"available_delta"`
	FrozenDelta       int64     `json:"frozen_delta"`
	PendingDelta      int64     `json:"pending_delta"`
	SourceKind        string    `json:"source_kind"`
	SourceID          string    `json:"source_id"`
	RuleVersionID     *int64    `json:"rule_version_id"`
	SettlementBatchID *int64    `json:"settlement_batch_id"`
	IdempotencyKey    string    `json:"idempotency_key"`
	OriginalEntryID   *int64    `json:"original_entry_id"`
	OccurredAt        time.Time `json:"occurred_at"`
	PostedAt          time.Time `json:"posted_at"`
	AuditID           *int64    `json:"audit_id"`
	CreatedAt         time.Time `json:"created_at"`
}

// OutboxEvent persists a projection delivery operation.
type OutboxEvent struct {
	ID                int64      `json:"id"`
	OperationID       string     `json:"operation_id"`
	Target            string     `json:"target"`
	TargetKey         string     `json:"target_key"`
	ProjectionVersion int64      `json:"projection_version"`
	Payload           []byte     `json:"payload"`
	Status            string     `json:"status"`
	LeaseUntil        *time.Time `json:"lease_until"`
	Attempts          int        `json:"attempts"`
	RemoteID          *string    `json:"remote_id"`
	Evidence          []byte     `json:"evidence"`
	CreatedAt         time.Time  `json:"created_at"`
}

// AuditLog records the human authorization for exceptional ledger actions.
type AuditLog struct {
	ID         int64     `json:"id"`
	ActorID    string    `json:"actor_id"`
	Reason     string    `json:"reason"`
	ApprovedBy *string   `json:"approved_by"`
	CreatedAt  time.Time `json:"created_at"`
}
