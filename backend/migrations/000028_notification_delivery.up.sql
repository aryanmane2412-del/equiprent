-- Communication system, part 2:
--   * delivery queue for push, email and SMS (worker-sent, retried, deduplicated)
--   * per-channel notification preferences
--   * in-app notifications deduplicated per outbox event
--   * booking reminder markers (rental ending soon)
--   * conversation_member(): who may use a conversation right now, so people
--     who lost access (a replaced operator, a removed employee) stop
--     receiving chat pushes, realtime events and list previews.

-- ---- preferences ------------------------------------------------------------------------------
ALTER TABLE notification_preferences
  ADD COLUMN email boolean NOT NULL DEFAULT true,
  ADD COLUMN sms   boolean NOT NULL DEFAULT false;

-- ---- in-app dedupe ----------------------------------------------------------------------------
ALTER TABLE notifications ADD COLUMN event_id uuid;
CREATE UNIQUE INDEX notifications_event_user_uq ON notifications (event_id, user_id) WHERE event_id IS NOT NULL;

-- ---- delivery queue ---------------------------------------------------------------------------
CREATE TABLE notification_deliveries (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  notification_id uuid REFERENCES notifications (id) ON DELETE SET NULL,
  channel         text NOT NULL CHECK (channel IN ('push', 'email', 'sms')),
  -- One delivery per user, channel and source (outbox event id, or a
  -- caller-chosen key): replays and retries never send twice.
  dedupe_key      text NOT NULL CHECK (length(dedupe_key) BETWEEN 1 AND 200),
  type            text NOT NULL CHECK (type ~ '^[a-z_.]+$'),
  title           text NOT NULL,
  body            text NOT NULL,
  data            jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
  status          text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'sending', 'sent', 'simulated', 'skipped', 'failed')),
  attempts        int NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  locked_until    timestamptz,
  provider        text,
  provider_ref    text CHECK (provider_ref IS NULL OR length(provider_ref) <= 200),
  last_error      text CHECK (last_error IS NULL OR length(last_error) <= 300),
  sent_at         timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, channel, dedupe_key)
);
CREATE INDEX notification_deliveries_due_idx ON notification_deliveries (next_attempt_at) WHERE status IN ('pending', 'sending');
CREATE INDEX notification_deliveries_user_idx ON notification_deliveries (user_id, created_at DESC);
CREATE TRIGGER notification_deliveries_updated_at BEFORE UPDATE ON notification_deliveries
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---- reminders --------------------------------------------------------------------------------
-- One row per reminder sent, so the hourly scan never repeats one. due_for
-- is the end time reminded about: an extension gets a fresh reminder.
CREATE TABLE booking_reminders (
  booking_id uuid NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
  kind       text NOT NULL CHECK (kind IN ('ending_soon')),
  due_for    timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (booking_id, kind, due_for)
);

-- ---- conversation membership ------------------------------------------------------------------
-- Mirrors chat access in Go (bookings.SidesFor / chat.join):
--   booking chats: customer org (customer, org_admin), owner org (owner),
--     fleet managers in scope of the machine, the current operator;
--   enquiries: customer org (customer, org_admin), owner org (owner, org_admin);
--   disputes: also platform operations / finance staff;
--   other kinds: recorded participants.
-- Suspended or deleted users are never members.
CREATE FUNCTION conversation_member(conv uuid, usr uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM users u WHERE u.id = usr AND u.status = 'active')
     AND EXISTS (
    SELECT 1
    FROM conversations c
    LEFT JOIN bookings b ON b.id = c.booking_id
    WHERE c.id = conv AND (
      (b.id IS NOT NULL AND b.status <> 'draft' AND (
           EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = usr AND ur.revoked_at IS NULL AND (
                     (ur.organization_id = b.customer_org_id AND ur.role_code IN ('customer', 'org_admin'))
                  OR (ur.organization_id = b.owner_org_id AND ur.role_code = 'owner')))
        OR EXISTS (SELECT 1 FROM fleet_manager_scope s WHERE s.user_id = usr AND s.equipment_id = b.equipment_id)
        OR EXISTS (SELECT 1 FROM operator_assignments a WHERE a.booking_id = b.id AND a.operator_user_id = usr
                     AND a.status IN ('assigned', 'accepted'))
        OR (c.kind = 'dispute' AND EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = usr AND ur.revoked_at IS NULL
                                             AND ur.role_code IN ('admin_ops', 'admin_finance')))))
      OR (c.equipment_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM user_roles ur WHERE ur.user_id = usr AND ur.revoked_at IS NULL AND (
                (ur.organization_id = c.customer_org_id AND ur.role_code IN ('customer', 'org_admin'))
             OR (ur.organization_id = c.owner_org_id AND ur.role_code IN ('owner', 'org_admin')))))
      OR (c.booking_id IS NULL AND c.equipment_id IS NULL AND EXISTS (
            SELECT 1 FROM conversation_participants p WHERE p.conversation_id = c.id AND p.user_id = usr))
    ));
$$;
