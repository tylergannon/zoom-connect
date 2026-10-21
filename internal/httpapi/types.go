package httpapi

import "time"

type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}
type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
type Call struct {
	ID            string     `json:"id"`
	Topic         string     `json:"topic"`
	Status        string     `json:"status"`
	Started       *time.Time `json:"started"`
	ScheduledAt   *time.Time `json:"scheduled_at,omitempty"`
	ZoomMeetingID string     `json:"zoom_meeting_id,omitempty"`
	IsOwner       bool       `json:"is_owner"`
	Ended         *time.Time `json:"ended,omitempty"`
	Host          Person     `json:"host"`
	Participants  []Person   `json:"participants"`
	LastSeq       int64      `json:"last_seq"`
	LatestEvent   *Event     `json:"latest_event,omitempty"`
	SharedBy      string     `json:"shared_by,omitempty"`
	IsParticipant bool       `json:"is_participant"`
	CanShare      bool       `json:"can_share"`
}
type Event struct {
	Seq         int64          `json:"seq"`
	TS          int64          `json:"ts"`
	Recv        int64          `json:"recv"`
	Kind        string         `json:"kind"`
	Participant *Person        `json:"participant,omitempty"`
	Data        map[string]any `json:"data"`
}
type Principal struct {
	User    User
	KeyID   string
	KeyName string
	IsKey   bool
}
