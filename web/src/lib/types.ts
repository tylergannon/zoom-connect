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
export type BoardCard = { lane?: string; text: string; source_seq?: number; x: number; y: number; w: number; h: number };
export type BoardDraft = { concept: string; title: string; lanes?: string[]; cards: BoardCard[]; links?: { from: number; to: number }[]; agent?: string };
export type WhiteboardStatus = {
	provider: 'zoom_whiteboard'; mode: 'preview' | 'native'; can_draft: boolean; can_publish: boolean;
	concepts: { id: string; name: string; lanes?: string[]; use: string }[];
	native: { enabled: boolean; api_base: string; token: 'supplied' | 'missing'; required_scopes: string[]; missing: string[]; not_sent_reason: string };
	meeting: { automatic: false; open: string; verification: string };
	latest: { draft: EventItem | null; published: EventItem | null; failed: EventItem | null; opened: EventItem | null; native_requests?: { method: string; url: string; scope: string; body?: unknown }[]; not_sent?: string[] };
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
