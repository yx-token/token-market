-- 生产可用率保留错误来源；旧程序不提供来源时使用未知值，不伪造有效观测。
-- 只增列与安全默认值，程序回退后旧 INSERT 仍可执行。
ALTER TABLE edge_attempts ADD COLUMN error_class text NOT NULL DEFAULT '';
