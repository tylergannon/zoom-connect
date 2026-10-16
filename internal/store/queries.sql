-- name: ListUsers :many
SELECT id, email, name, is_admin, created_at, last_login_at FROM users ORDER BY name;

-- name: GetUserByEmail :one
SELECT id, email, name, is_admin, created_at, last_login_at FROM users WHERE lower(email)=lower($1);

-- name: GetUserByID :one
SELECT id, email, name, is_admin, created_at, last_login_at FROM users WHERE id=$1;

-- name: CreateSession :exec
INSERT INTO sessions(id,user_id,expires_at) VALUES($1,$2,$3);

-- name: GetSession :one
SELECT u.id,u.email,u.name,u.is_admin FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=$1 AND s.expires_at>now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id=$1;

-- name: CreateKey :one
INSERT INTO api_keys(id,user_id,name,key_hash,prefix) VALUES($1,$2,$3,$4,$5) RETURNING id,user_id,name,key_hash,prefix,created_at,last_used_at,revoked_at;

-- name: ListKeys :many
SELECT id,user_id,name,key_hash,prefix,created_at,last_used_at,revoked_at FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC;

-- name: FindKey :one
SELECT id,user_id,name,key_hash,prefix,created_at,last_used_at,revoked_at FROM api_keys WHERE key_hash=$1 AND revoked_at IS NULL;

-- name: TouchKey :exec
UPDATE api_keys SET last_used_at=now() WHERE id=$1 AND revoked_at IS NULL;

-- name: RevokeKey :exec
UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL;

-- name: CreateCall :exec
INSERT INTO calls(id,topic,status,started,ended,host_user_id,last_seq,event_path) VALUES($1,$2,$3,$4,$5,$6,$7,$8);

-- name: ListCalls :many
SELECT c.id,c.topic,c.status,c.started,c.ended,c.host_user_id,c.last_seq,c.event_path,
       u.email AS host_email,u.name AS host_name,
       EXISTS(SELECT 1 FROM call_participants p WHERE p.call_id=c.id AND p.user_id=$1) AS is_participant,
       COALESCE((SELECT u2.name FROM shares s JOIN users u2 ON u2.id=s.granted_by WHERE s.call_id=c.id AND s.email=(SELECT email FROM users WHERE id=$1) LIMIT 1), '')::text AS shared_by
FROM calls c JOIN users u ON u.id=c.host_user_id
WHERE EXISTS(SELECT 1 FROM call_participants p WHERE p.call_id=c.id AND p.user_id=$1)
   OR EXISTS(SELECT 1 FROM shares s WHERE s.call_id=c.id AND (s.grantee_user_id=$1 OR s.email=(SELECT email FROM users WHERE id=$1)))
ORDER BY c.started DESC;

-- name: GetCall :one
SELECT c.id,c.topic,c.status,c.started,c.ended,c.host_user_id,c.last_seq,c.event_path,
       u.email AS host_email,u.name AS host_name
FROM calls c JOIN users u ON u.id=c.host_user_id WHERE c.id=$1;

-- name: CanAccessCall :one
SELECT EXISTS(SELECT 1 FROM call_participants p WHERE p.call_id=$1 AND p.user_id=$2)
   OR EXISTS(SELECT 1 FROM shares s WHERE s.call_id=$1 AND (s.grantee_user_id=$2 OR s.email=(SELECT email FROM users WHERE id=$2)));

-- name: ListParticipants :many
SELECT p.participant_id,p.user_id,p.name,p.email FROM call_participants p WHERE p.call_id=$1 ORDER BY p.name;

-- name: AddParticipant :exec
INSERT INTO call_participants(call_id,participant_id,user_id,name,email) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING;

-- name: UpdateCallSeq :exec
UPDATE calls SET last_seq=$2 WHERE id=$1;

-- name: StopCall :exec
UPDATE calls SET status=$2,ended=now() WHERE id=$1 AND status='live';

-- name: SearchPeople :many
SELECT id,email,name,is_admin,created_at,last_login_at FROM users WHERE lower(email) LIKE '%'||lower($1)||'%' OR lower(name) LIKE '%'||lower($1)||'%' ORDER BY name LIMIT 20;

-- name: CreateShare :one
INSERT INTO shares(id,call_id,email,grantee_user_id,granted_by)
VALUES($1,$2,lower($3),(SELECT id FROM users WHERE lower(email)=lower($3)),$4)
ON CONFLICT(call_id,email) DO NOTHING
RETURNING id,call_id,email,grantee_user_id,granted_by,created_at;

-- name: ListShares :many
SELECT s.id,s.call_id,s.email,s.grantee_user_id,s.granted_by,s.created_at,u.name AS granted_by_name,g.name AS name
FROM shares s JOIN users u ON u.id=s.granted_by LEFT JOIN users g ON g.id=s.grantee_user_id WHERE s.call_id=$1 ORDER BY s.created_at;

-- name: DeleteShare :exec
DELETE FROM shares WHERE id=$1 AND call_id=$2 AND granted_by=$3;

-- name: CountCalls :one
SELECT count(*) FROM calls;

-- name: SetSetting :exec
INSERT INTO settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now();

-- name: GetSetting :one
SELECT value FROM settings WHERE key=$1;
