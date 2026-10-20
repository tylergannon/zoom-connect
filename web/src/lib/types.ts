export type User = { id: string; email: string; name: string; is_admin: boolean };
export type Person = { id: string; name: string; email?: string };
export type Call = {
	id: string; topic: string; status: 'scheduled' | 'live' | 'stopped' | 'interrupted' | 'cancelled'; started: string | null;
	scheduled_at?: string; zoom_meeting_id?: string; is_owner: boolean;
	ended?: string; host: Person; participants: Person[]; last_seq: number;
	shared_by?: string; is_participant: boolean; can_share: boolean;
	latest_event?: EventItem;
};
export type EventItem = {
	seq: number; ts: number; recv?: number; kind: string; participant?: Person;
	data: { text?: string; lang?: string; final?: boolean; [key: string]: unknown };
};
export type Share = { id: string; email: string; name?: string; granted_by: string; granted_by_name: string; created_at: string; can_remove: boolean };
export type APIKey = { id: string; name: string; prefix: string; created_at: string; last_used_at?: string; revoked_at?: string };
export type ZoomSettings = {
	mode: 'demo' | string; connected: boolean; webhook_url: string; last_webhook?: string;
	live_streams: number; calls_this_week: number;
	credentials: { client_id?: string; client_secret_suffix?: string; webhook_secret_configured: boolean };
	capture: { transcript: boolean; chat: boolean };
	webhooks: { at: string; event: string; topic?: string; outcome: string }[];
};
export type Artifact = {
	id: string; call_id: string; title: string; version: number; bytes: number;
	created_at: string; updated_at: string; created_by: Person; updated_by: Person;
	updated_via?: string; viewer_url: string;
};
export type ArtifactSnapshot = Artifact & { html: string };
