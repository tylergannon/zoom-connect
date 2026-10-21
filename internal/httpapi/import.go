package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/tylergannon/zoom-connect/internal/store/db"
)

// Import legacy files once, atomically per call. Clear the old path only after
// all events are durable. Files remain intact as a backup, never written again.
func (a *Server) importArchives(ctx context.Context) error {
	archives, err := a.s.Q.ListLegacyArchives(ctx)
	if err != nil {
		return err
	}
	for _, archive := range archives {
		if err := a.importArchive(ctx, archive.ID, archive.EventPath); err != nil {
			return fmt.Errorf("import call %s: %w", archive.ID, err)
		}
	}
	return nil
}
func (a *Server) importArchive(ctx context.Context, id, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tx, err := a.s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var lockedPath string
	if err = tx.QueryRow(ctx, `SELECT event_path FROM calls WHERE id=$1 FOR UPDATE`, id).Scan(&lockedPath); err != nil {
		return err
	}
	if lockedPath == "" {
		return nil
	}
	q := a.s.Q.WithTx(tx)
	rows, err := q.ListCallEvents(ctx, db.ListCallEventsParams{CallID: id})
	if err != nil {
		return err
	}
	existing := map[int64]Event{}
	for _, r := range rows {
		e, err := decodeEvent(r.Seq, r.Ts, r.Recv, r.Kind, r.Participant, r.Data)
		if err != nil {
			return err
		}
		existing[e.Seq] = e
	}
	dec := json.NewDecoder(f)
	var last int64
	for {
		var ev Event
		err := dec.Decode(&ev)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("decode after event %d: %w", last, err)
		}
		if ev.Seq != last+1 {
			return fmt.Errorf("expected event %d, got %d", last+1, ev.Seq)
		}
		last = ev.Seq
		if prior, ok := existing[ev.Seq]; ok {
			if !reflect.DeepEqual(prior, ev) {
				return fmt.Errorf("event %d conflicts with database", ev.Seq)
			}
			continue
		}
		params, err := eventParams(id, ev)
		if err != nil {
			return err
		}
		if err = q.InsertCallEvent(ctx, params); err != nil {
			return err
		}
	}
	for seq := range existing {
		if seq > last {
			return fmt.Errorf("database event %d is absent from archive", seq)
		}
	}
	// Refuse truncation: metadata may include events absent from an incomplete file.
	var advertised int64
	if err = tx.QueryRow(ctx, `SELECT last_seq FROM calls WHERE id=$1`, id).Scan(&advertised); err != nil {
		return err
	}
	if advertised > last {
		return fmt.Errorf("archive ends at %d, metadata expects %d", last, advertised)
	}
	if _, err = tx.Exec(ctx, `UPDATE calls SET last_seq=$2,event_path='' WHERE id=$1`, id, last); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
