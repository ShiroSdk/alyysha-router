// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package freeresilience

import (
	"context"
	"fmt"
	"time"
)

// RecoveryNotifier is the optional store capability that reports a provider
// rejoining rotation. The SQLite repo implements it through the same Notify
// row the notifications scanner serves; fakes without it skip the event.
type RecoveryNotifier interface {
	Notify(ctx context.Context, id, at, dedupeKey, typ, severity, title, body string) (string, error)
}

// NotifyRecovery emits one recovery event per rejoined provider. The
// dedupeKey <provider>#recovery keeps a flapping provider from spamming the
// notifications feed: the same recovery within the dedupe window records
// once.
func NotifyRecovery(ctx context.Context, store RecoveryNotifier, providers []string, now time.Time) []string {
	if store == nil || len(providers) == 0 {
		return nil
	}
	at := now.UTC().Format(time.RFC3339)
	created := make([]string, 0, len(providers))
	for _, provider := range providers {
		dedupeKey := fmt.Sprintf("%s#recovery", provider)
		title := fmt.Sprintf("%s recovered — back in rotation", provider)
		body := fmt.Sprintf("The cooldown window for %s expired and the provider rejoined the fallback chain. The next request tests it naturally; another failure re-cools it with a longer window.", provider)
		if _, err := store.Notify(ctx, "", at, dedupeKey, "recovery", "info", title, body); err != nil {
			continue
		}
		created = append(created, dedupeKey)
	}
	return created
}
