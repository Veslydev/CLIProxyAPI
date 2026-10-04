package controlplane

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func metadataString(a *auth.Auth, key string) string { v, _ := a.Metadata[key].(string); return v }

// Identity deliberately excludes tokens, email and mutable display labels.
func identity(a *auth.Auth) string {
	subject := metadataString(a, "account_id")
	if subject == "" {
		subject = metadataString(a, "chatgpt_account_id")
	}
	if subject == "" {
		subject = metadataString(a, "user_id")
	}
	if subject == "" {
		subject = metadataString(a, "account_uuid")
	}
	workspace := metadataString(a, "workspace_id")
	if workspace == "" {
		workspace = metadataString(a, "organization_id")
	}
	if workspace == "" {
		workspace = metadataString(a, "organization_uuid")
	}
	if subject != "" {
		return hash(a.Provider + "\x00" + subject + "\x00" + workspace)
	}
	return hash(a.Provider + "\x00credential\x00" + a.ID)
}

func (s *Store) SyncAccounts(auths []*auth.Auth) error {
	return s.mutate("", "", func(tx *sql.Tx) error {
		for _, a := range auths {
			if a == nil || a.ID == "" {
				continue
			}
			ref := a.Provider + ":" + a.ID
			fingerprint := identity(a)
			var id, previousIdentity string
			err := tx.QueryRow("SELECT account,identity FROM credentials WHERE ref=?", ref).Scan(&id, &previousIdentity)
			// Reusing a filename for a different upstream principal must not inherit
			// the former account's API-key scope or continuation ownership.
			fallback := hash(a.Provider + "\x00credential\x00" + a.ID)
			if err == nil && previousIdentity != fingerprint && previousIdentity != fallback && fingerprint != fallback {
				id = ""
				err = sql.ErrNoRows
			}
			if err == nil && fingerprint == fallback && previousIdentity != "" {
				fingerprint = previousIdentity
			}
			if errors.Is(err, sql.ErrNoRows) {
				err = tx.QueryRow("SELECT account FROM credentials WHERE identity=? ORDER BY ref LIMIT 1", fingerprint).Scan(&id)
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if id == "" {
				id = uuid.NewString()
			}
			account := s.state.Load().Accounts[id]
			account.ID = id
			account.Provider = a.Provider
			if identity(a) != fallback {
				account.IdentityEvidence = "provider"
			} else if account.IdentityEvidence == "" {
				account.IdentityEvidence = "credential_reference"
			}
			account.Label = a.Label
			if account.Label == "" {
				account.Label = a.ID
			}
			if plan := metadataString(a, "plan_type"); plan != "" {
				account.Plan = plan
			}
			workspace := metadataString(a, "workspace_id")
			if workspace == "" {
				workspace = metadataString(a, "organization_uuid")
			}
			if workspace != "" {
				account.Workspace = workspace
			}
			account.Health = "active"
			if a.Disabled {
				account.Health = "disabled"
			} else if a.Unavailable {
				account.Health = "limited"
			} else if a.LastError != nil {
				account.Health = "error"
			}
			if err = put(tx, "account", id, account); err != nil {
				return err
			}
			if _, err = tx.Exec("INSERT INTO credentials(ref,identity,account) VALUES(?,?,?) ON CONFLICT(ref) DO UPDATE SET identity=excluded.identity,account=excluded.account", ref, fingerprint, id); err != nil {
				return err
			}
			poolID := "default-" + a.Provider
			var pool Pool
			{
				var data string
				err = tx.QueryRow("SELECT data FROM entities WHERE kind='pool' AND id=?", poolID).Scan(&data)
				if err == nil {
					if err = decode(data, &pool); err != nil {
						return err
					}
				} else if errors.Is(err, sql.ErrNoRows) {
					pool = Pool{ID: poolID, Name: poolID, Provider: a.Provider, Strategy: "native", Sticky: true, Accounts: []string{}, Models: []string{}}
				} else {
					return err
				}
			}
			if !contains(pool.Accounts, id) && (errors.Is(err, sql.ErrNoRows) || s.state.Load().Accounts[id].ID == "") {
				pool.Accounts = append(pool.Accounts, id)
				if err = put(tx, "pool", poolID, pool); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func contains(items []string, item string) bool {
	for _, v := range items {
		if v == item {
			return true
		}
	}
	return false
}

func (s *Store) SaveAccount(a Account) error {
	return s.mutate("account.update", a.ID, func(tx *sql.Tx) error {
		old, ok := s.state.Load().Accounts[a.ID]
		if !ok {
			return errors.New("unknown account")
		}
		if old.ReplacedBy != "" {
			return errors.New("replaced account metadata is read-only")
		}
		old.Paused = a.Paused
		old.WarmEnabled = a.WarmEnabled
		return put(tx, "account", a.ID, old)
	})
}

func (s *Store) SavePool(p Pool) error {
	normalizePool(&p)
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	return s.mutate("pool.update", p.ID, func(tx *sql.Tx) error {
		if p.Provider == "" || p.Name == "" || !contains(Strategies, p.Strategy) || p.ReservePercent < 0 || p.ReservePercent >= 100 {
			return errors.New("invalid pool configuration")
		}
		if p.Health.MaxInFlight < 0 || p.Health.RateLimitCooldownSeconds < 0 || p.Health.RateLimitCooldownSeconds > 31536000 || p.Health.ErrorCooldownSeconds < 0 || p.Health.ErrorCooldownSeconds > 31536000 || p.Health.ConsecutiveFailureLimit < 0 {
			return errors.New("invalid pool health policy")
		}
		for _, id := range p.Accounts {
			a, ok := s.state.Load().Accounts[id]
			if !ok || a.Provider != p.Provider || a.ReplacedBy != "" {
				return fmt.Errorf("account %s does not belong to provider", id)
			}
		}
		if p.Warm.Enabled && (p.Warm.Model == "" || p.Warm.WindowSeconds < 0 || p.Warm.WindowSeconds > 31536000 || p.Warm.IdleSeconds < 0 || p.Warm.IdleSeconds > 31536000 || p.Warm.CooldownSeconds < 60 || p.Warm.CooldownSeconds > 31536000 || p.Warm.SpacingSeconds < 0 || p.Warm.SpacingSeconds > 31536000 || p.Warm.JitterSeconds < 0 || p.Warm.JitterSeconds > 3600 || (p.Warm.WindowSeconds > 0 && p.Warm.JitterSeconds >= p.Warm.WindowSeconds)) {
			return errors.New("invalid warm-up policy")
		}
		if old, ok := s.state.Load().Pools[p.ID]; ok && old.Provider != p.Provider {
			return errors.New("pool provider cannot change")
		}
		if p.Warm.ResetMode != "" && p.Warm.ResetMode != "phased" && p.Warm.ResetMode != "immediate" {
			return errors.New("invalid reset warm-up mode")
		}
		if p.Warm.Enabled && len(p.Models) > 0 && !contains(p.Models, p.Warm.Model) {
			return errors.New("warm model must be allowed by pool")
		}
		return put(tx, "pool", p.ID, p)
	})
}

func validateKey(k APIKey, st *snapshot) error {
	if k.Name == "" || len(k.Bindings) == 0 || k.RequestLimit < 0 || k.TokenLimit < 0 || k.CostLimit < 0 {
		return errors.New("key requires a name, provider bindings and nonnegative limits")
	}
	for provider, b := range k.Bindings {
		if len(b.Accounts)+len(b.Pools) == 0 {
			return errors.New("empty provider binding")
		}
		for _, id := range b.Accounts {
			a, ok := st.Accounts[id]
			if !ok || a.Provider != provider || a.ReplacedBy != "" {
				return errors.New("invalid provider account binding")
			}
		}
		for _, id := range b.Pools {
			p, ok := st.Pools[id]
			if !ok || p.Provider != provider {
				return errors.New("invalid provider pool binding")
			}
		}
	}
	return nil
}

func (s *Store) CreateKey(k APIKey) (APIKey, string, error) {
	normalizeKey(&k)
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return k, "", err
	}
	secret := "cp_" + base64.RawURLEncoding.EncodeToString(raw[:])
	k.ID = uuid.NewString()
	k.Prefix = secret[:11]
	k.Requests = 0
	k.Tokens = 0
	k.Cost = 0
	k.LastUsed = time.Time{}
	k.Revoked = false
	err := s.mutate("key.create", k.ID, func(tx *sql.Tx) error {
		if err := validateKey(k, s.state.Load()); err != nil {
			return err
		}
		if err := put(tx, "key", k.ID, k); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO key_secrets(hash,id) VALUES(?,?)", hash(secret), k.ID)
		return err
	})
	if err != nil {
		return APIKey{}, "", err
	}
	return k, secret, nil
}

func (s *Store) SaveKey(k APIKey) error {
	normalizeKey(&k)
	return s.mutate("key.update", k.ID, func(tx *sql.Tx) error {
		old, ok := s.state.Load().Keys[k.ID]
		if !ok {
			return errors.New("unknown key")
		}
		if err := validateKey(k, s.state.Load()); err != nil {
			return err
		}
		k.Prefix = old.Prefix
		k.Requests = old.Requests
		k.Tokens = old.Tokens
		k.Cost = old.Cost
		k.LastUsed = old.LastUsed
		return put(tx, "key", k.ID, k)
	})
}

func (s *Store) Key(secret string) (APIKey, bool) {
	st := s.state.Load()
	id, ok := st.Secrets[hash(secret)]
	if !ok {
		return APIKey{}, false
	}
	k, ok := st.Keys[id]
	return k, ok
}

func (s *Store) Admit(secret string) (APIKey, error) {
	k, ok := s.Key(secret)
	if !ok {
		return APIKey{}, errors.New("invalid inference key")
	}
	return s.admitKey(k.ID)
}

func (s *Store) admitKey(id string) (APIKey, error) {
	var key APIKey
	updates := make(map[string]APIKey)
	err := s.mutateAccounting(func(tx *sql.Tx) error {
		k, ok := s.state.Load().Keys[id]
		if !ok || k.Revoked || (!k.ExpiresAt.IsZero() && !s.now().Before(k.ExpiresAt)) {
			return errors.New("invalid or expired inference key")
		}
		if (k.RequestLimit > 0 && k.Requests >= k.RequestLimit) || (k.TokenLimit > 0 && k.Tokens >= k.TokenLimit) || (k.CostLimit > 0 && k.Cost >= k.CostLimit) {
			return errors.New("inference key limit reached")
		}
		k.Requests++
		k.LastUsed = s.now()
		key = k
		updates[id] = k
		return put(tx, "key", k.ID, k)
	}, nil, updates)
	return key, err
}

func (s *Store) SaveSettings(settings Settings) error {
	if settings.PhasePreference < 0 || settings.PhasePreference > 0.05 {
		return errors.New("phase preference must be between 0 and 0.05")
	}
	if !contains(Strategies, settings.Strategy) || settings.QuotaFreshSeconds <= 0 || settings.StickySeconds < 0 || settings.InputPricePerMillion < 0 || settings.OutputPricePerMillion < 0 || settings.StickyMinRemainingPercent < 0 || settings.StickyMinRemainingPercent > 100 || settings.FailurePenalty < 0 || settings.InFlightPenalty < 0 {
		return errors.New("invalid routing settings")
	}
	if settings.RetentionDays < 0 || settings.RetentionDays > 36500 {
		return errors.New("invalid retention policy")
	}
	for provider, policy := range settings.Providers {
		if provider == "" || policy.QuotaRefreshSeconds < 0 || policy.QuotaRefreshSeconds > 86400 {
			return errors.New("invalid provider policy")
		}
	}
	seen := map[string]bool{}
	for _, price := range settings.Pricing {
		key := price.Provider + ":" + price.Model
		if price.Model == "" || price.InputPerMillion < 0 || price.OutputPerMillion < 0 || seen[key] {
			return errors.New("invalid model pricing")
		}
		seen[key] = true
	}
	return s.mutate("settings.update", "routing", func(tx *sql.Tx) error { return put(tx, "settings", "routing", settings) })
}

func credentialRef(provider, id string) string { return strings.ToLower(provider) + ":" + id }

func normalizePool(p *Pool) {
	if p.Accounts == nil {
		p.Accounts = []string{}
	}
	if p.Models == nil {
		p.Models = []string{}
	}
}

func normalizeKey(k *APIKey) {
	k.Bindings = maps.Clone(k.Bindings)
	if k.Models == nil {
		k.Models = []string{}
	}
	for provider, binding := range k.Bindings {
		if binding.Accounts == nil {
			binding.Accounts = []string{}
		}
		if binding.Pools == nil {
			binding.Pools = []string{}
		}
		k.Bindings[provider] = binding
	}
}
