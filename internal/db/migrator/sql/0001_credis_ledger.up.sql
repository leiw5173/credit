CREATE TABLE ledger_accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    forum_user_id bigint NOT NULL UNIQUE,
    available_balance bigint NOT NULL DEFAULT 0,
    frozen_balance bigint NOT NULL DEFAULT 0 CHECK (frozen_balance >= 0),
    pending_balance bigint NOT NULL DEFAULT 0 CHECK (pending_balance >= 0),
    projection_version bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id text NOT NULL,
    reason text NOT NULL,
    approved_by text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES ledger_accounts(id),
    action text NOT NULL CHECK (action IN ('earn','freeze','consume','release','pending_settle','refund','reversal','restore','opening','manual_adjustment')),
    available_delta bigint NOT NULL DEFAULT 0,
    frozen_delta bigint NOT NULL DEFAULT 0,
    pending_delta bigint NOT NULL DEFAULT 0,
    source_kind text NOT NULL,
    source_id text NOT NULL,
    rule_version_id bigint,
    settlement_batch_id bigint,
    idempotency_key text NOT NULL,
    original_entry_id bigint REFERENCES ledger_entries(id),
    occurred_at timestamptz NOT NULL,
    posted_at timestamptz NOT NULL DEFAULT now(),
    audit_id bigint REFERENCES audit_logs(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, idempotency_key),
    CHECK (available_delta <> 0 OR frozen_delta <> 0 OR pending_delta <> 0)
);

CREATE TABLE outbox_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    operation_id text NOT NULL UNIQUE,
    target text NOT NULL,
    target_key text NOT NULL,
    projection_version bigint NOT NULL DEFAULT 0,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    remote_id text,
    evidence jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ledger_accounts_projection_version_idx ON ledger_accounts (projection_version);
CREATE INDEX ledger_entries_account_occurred_at_idx ON ledger_entries (account_id, occurred_at);
CREATE INDEX ledger_entries_source_idx ON ledger_entries (source_kind, source_id);
CREATE INDEX ledger_entries_original_entry_id_idx ON ledger_entries (original_entry_id);
CREATE INDEX ledger_entries_rule_version_id_idx ON ledger_entries (rule_version_id);
CREATE INDEX ledger_entries_settlement_batch_id_idx ON ledger_entries (settlement_batch_id);
CREATE INDEX ledger_entries_audit_id_idx ON ledger_entries (audit_id);
CREATE INDEX outbox_events_pending_idx ON outbox_events (status, lease_until, created_at);
CREATE INDEX outbox_events_target_idx ON outbox_events (target, target_key, projection_version);

CREATE FUNCTION prevent_ledger_entry_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries are immutable';
END;
$$;

CREATE TRIGGER ledger_entries_immutable
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_entry_mutation();

REVOKE UPDATE, DELETE ON ledger_entries FROM PUBLIC;
