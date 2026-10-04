package controlplane

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type httpObservationKey struct{}
type httpObservation struct{ published atomic.Bool }

// ObserveInferenceHTTP records failures that never published executor usage.
// It never reads bodies, credentials, query strings or upstream failure text.
// Published execution attempts remain authoritative for billing and routing.
func (s *Store) ObserveInferenceHTTP() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/v1/") && !strings.HasPrefix(path, "/v1beta/") && !strings.HasPrefix(path, "/openai/v1/") && !strings.HasPrefix(path, "/backend-api/codex/") {
			c.Next()
			return
		}
		started := s.now()
		clockStarted := time.Now()
		observation := &httpObservation{}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), httpObservationKey{}, observation))
		c.Next()
		status := c.Writer.Status()
		if status < 400 || observation.published.Load() || c.FullPath() == "" {
			return
		}
		entry := RequestLog{ID: uuid.NewString(), At: started, Status: status, LatencyMS: time.Since(clockStarted).Milliseconds(), Phase: "http_rejection", Route: c.Request.Method + " " + c.FullPath()}
		if keyID := RequestFromContext(c.Request.Context()).KeyID; keyID != "" {
			entry.APIKey = keyID
		} else if key, ok := s.Key(inferenceSecret(c.Request)); ok {
			entry.APIKey = key.ID
		}
		switch status {
		case 401, 403:
			entry.ErrorCategory = "authentication_or_scope"
		case 429:
			entry.ErrorCategory = "admission_or_rate_limit"
		case 400, 404, 422:
			entry.ErrorCategory = "request"
		default:
			entry.ErrorCategory = "dispatch"
		}
		if err := s.recordHTTPRejection(entry); err != nil {
			s.accountingFailed.Store(true)
			log.WithError(err).Error("control-plane rejected request persistence failed; request admission disabled")
		}
	}
}

func (s *Store) recordHTTPRejection(entry RequestLog) error {
	data, err := encode(entry)
	if err != nil {
		return err
	}
	return s.transaction("", "", func(tx *sql.Tx) error {
		if _, errInsert := tx.Exec("INSERT INTO requests(id,at,provider,model,account,pool,api_key,status,tokens,latency,data) VALUES(?,?,?,?,?,?,?,?,?,?,?)", entry.ID, entry.At.Unix(), "", "", "", "", entry.APIKey, entry.Status, 0, entry.LatencyMS, data); errInsert != nil {
			return errInsert
		}
		// Empty provider/account denotes a local rejection, not fabricated upstream usage.
		_, errRollup := tx.Exec(`INSERT INTO rollups(day,provider,model,account,pool,api_key,requests,failures,tokens,latency,cost) VALUES(?,'','','','',?,1,1,0,?,0) ON CONFLICT(day,provider,model,account,pool,api_key) DO UPDATE SET requests=requests+1,failures=failures+1,latency=latency+excluded.latency`, entry.At.UTC().Format("2006-01-02"), entry.APIKey, entry.LatencyMS)
		return errRollup
	}, func() error { return nil })
}
