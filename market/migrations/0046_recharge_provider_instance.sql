-- 0046 Pin online recharge orders to the host payment provider instance.
-- Keep historical orders NULL so they can use the channel's current default instance.

ALTER TABLE recharge_orders ADD COLUMN IF NOT EXISTS provider_instance_id bigint;
