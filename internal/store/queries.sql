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
SELECT c.id,c.topic,c.status,c.started,c.ended,c.host_user_id,c.last_seq,c.event_path,c.scheduled_at,c.zoom_meeting_id,c.zoom_meeting_uuid,
       u.email AS host_email,u.name AS host_name,
       EXISTS(SELECT 1 FROM call_participants p WHERE p.call_id=c.id AND p.user_id=$1) AS is_participant,
       COALESCE((SELECT u2.name FROM shares s JOIN users u2 ON u2.id=s.granted_by WHERE s.call_id=c.id AND s.email=(SELECT email FROM users WHERE id=$1) LIMIT 1), '')::text AS shared_by
FROM calls c JOIN users u ON u.id=c.host_user_id
WHERE EXISTS(SELECT 1 FROM call_participants p WHERE p.call_id=c.id AND p.user_id=$1)
   OR EXISTS(SELECT 1 FROM shares s WHERE s.call_id=c.id AND (s.grantee_user_id=$1 OR s.email=(SELECT email FROM users WHERE id=$1)))
ORDER BY COALESCE(c.started,c.scheduled_at) DESC NULLS LAST;

-- name: GetCall :one
SELECT c.id,c.topic,c.status,c.started,c.ended,c.host_user_id,c.last_seq,c.event_path,c.scheduled_at,c.zoom_meeting_id,c.zoom_meeting_uuid,
       u.email AS host_email,u.name AS host_name
FROM calls c JOIN users u ON u.id=c.host_user_id WHERE c.id=$1;

-- name: CanAccessCall :one
SELECT EXISTS(SELECT 1 FROM call_participants p WHERE p.call_id=sqlc.arg(call_id) AND p.user_id=sqlc.narg(user_id))
   OR EXISTS(SELECT 1 FROM shares s WHERE s.call_id=sqlc.arg(call_id) AND (s.grantee_user_id=sqlc.narg(user_id) OR s.email=(SELECT email FROM users WHERE id=sqlc.narg(user_id))));

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

-- name: ReserveEventSeq :one
UPDATE calls SET last_seq=last_seq+1 WHERE id=$1 RETURNING last_seq;

-- name: InsertCallEvent :exec
INSERT INTO call_events(call_id,seq,ts,recv,kind,participant,data) VALUES($1,$2,$3,$4,$5,$6,$7);

-- name: ListCallEvents :many
SELECT seq,ts,recv,kind,participant,data FROM call_events WHERE call_id=$1 AND seq>$2 ORDER BY seq;

-- name: LatestTranscript :one
SELECT seq,ts,recv,kind,participant,data FROM call_events WHERE call_id=$1 AND kind='transcript' ORDER BY seq DESC LIMIT 1;

-- name: AdvanceEventSeq :exec
UPDATE calls SET last_seq=GREATEST(last_seq,$2) WHERE id=$1;

-- name: ListLegacyArchives :many
SELECT id,event_path FROM calls WHERE event_path<>'';

-- name: LockCallStatus :one
SELECT status FROM calls WHERE id=$1 FOR UPDATE;

-- name: CountCallArtifacts :one
SELECT count(*) FROM call_artifacts WHERE call_id=$1;

-- name: CreateCallArtifact :exec
INSERT INTO call_artifacts(id,call_id,version,created_by) VALUES($1,$2,1,$3);

-- name: AdvanceCallArtifact :one
UPDATE call_artifacts SET version=version+1 WHERE id=$1 AND call_id=$2 AND version=$3 RETURNING version;

-- name: CallArtifactVersion :one
SELECT version FROM call_artifacts WHERE id=$1 AND call_id=$2;

-- name: InsertArtifactVersion :exec
INSERT INTO call_artifact_versions(artifact_id,version,title,html,author_id,key_name) VALUES($1,$2,$3,$4,$5,$6);

-- name: ListArtifactVersions :many
SELECT v.version,v.title,octet_length(v.html)::bigint AS bytes,v.key_name,v.created_at,u.id AS author_id,u.name AS author_name
FROM call_artifact_versions v JOIN call_artifacts a ON a.id=v.artifact_id JOIN users u ON u.id=v.author_id
WHERE v.artifact_id=$1 AND a.call_id=$2 AND v.version<$3
ORDER BY v.version DESC LIMIT $4;

-- name: GetCallArtifact :one
SELECT a.id,a.call_id,a.version,a.created_at,cu.id AS created_by_id,cu.name AS created_by_name,
       v.title,v.html,v.key_name,v.created_at AS updated_at,uu.id AS updated_by_id,uu.name AS updated_by_name
FROM call_artifacts a
JOIN call_artifact_versions v ON v.artifact_id=a.id AND v.version=a.version
JOIN users cu ON cu.id=a.created_by JOIN users uu ON uu.id=v.author_id
WHERE a.id=$1 AND a.call_id=$2;

-- name: ListCallArtifacts :many
SELECT a.id,a.call_id,a.version,a.created_at,cu.id AS created_by_id,cu.name AS created_by_name,
       v.title,octet_length(v.html)::bigint AS bytes,v.key_name,v.created_at AS updated_at,uu.id AS updated_by_id,uu.name AS updated_by_name
FROM call_artifacts a
JOIN call_artifact_versions v ON v.artifact_id=a.id AND v.version=a.version
JOIN users cu ON cu.id=a.created_by JOIN users uu ON uu.id=v.author_id
WHERE a.call_id=$1 ORDER BY a.created_at,a.id;

-- name: GetArtifactVersion :one
SELECT v.version,v.title,v.html,v.key_name,v.created_at,u.id AS author_id,u.name AS author_name
FROM call_artifact_versions v JOIN call_artifacts a ON a.id=v.artifact_id JOIN users u ON u.id=v.author_id
WHERE v.artifact_id=$1 AND a.call_id=$2 AND v.version=$3;
