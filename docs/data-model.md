# Data model

[繁體中文](data-model.zh-TW.md)

Core relationships: a User owns Devices; a Group owns Members and Devices; a TransferTask creates one TransferTarget per receiving Device; a File creates one FileTarget per receiver and consists of FileChunks. Message, ContentKey, DeliveryStatus, Metric, Notification, and AuditLog records link back to a task or device.

`transfer_tasks.idempotency_key` is unique with the sender device, and `request_fingerprint` detects key reuse. File chunks are unique by `(file_id, chunk_index)`, and metrics are unique by `event_id`. `transfer_executions` preserves every target attempt instead of overwriting terminal history.

Transfer state changes are limited to the v1.3 matrix defined by the domain module. Terminal states cannot return to execution, and delivered content can only become read. PostgreSQL row-lock transactions protect every state update.
