package appdb

import (
	"context"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (h *Handle) Navigation(ctx context.Context, k persistence.Session) ([]persistence.Landmark, error) {
	// The window function scans the small user-message index in chronological
	// order. It returns at most 24 previews without fetching any message bodies.
	rows, err := h.QueryContext(ctx, `WITH scope AS (SELECT CASE WHEN ?='local' AND EXISTS(SELECT 1 FROM history_heads WHERE env='@managed' AND provider=? AND session=?) THEN '@managed' ELSE ? END AS env),
 ranked AS (SELECT a.row_id,a.preview,t.position AS tp,a.position AS ip,row_number() OVER (ORDER BY t.position,a.position)-1 AS n, count(*) OVER () AS total FROM message_anchors a JOIN turns t USING(env,provider,session,turn) JOIN scope USING(env) WHERE a.provider=? AND a.session=?)
 SELECT row_id,preview,tp,ip FROM ranked WHERE total<=24 OR n=total-1 OR (n*23+total-2)/(total-1) != ((n+1)*23+total-2)/(total-1) ORDER BY n`, k.Env, k.Provider, k.ID, k.Env, k.Provider, k.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []persistence.Landmark
	for rows.Next() {
		var a persistence.Landmark
		if err := rows.Scan(&a.ID, &a.Preview, &a.Position.Turn, &a.Position.Item); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
