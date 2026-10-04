package controlplane

import (
	"database/sql"
	"errors"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// ReauthValidator compares only fresh native principal/workspace evidence before
// credential persistence. Target selection, email and OAuth state are not proof.
func (s *Store) ReauthValidator(target, provider string) (func(*auth.Auth) error, error) {
	account, ok := s.state.Load().Accounts[target]
	if !ok || account.Provider != provider || account.ReplacedBy != "" {
		return nil, errors.New("invalid re-auth target")
	}
	if account.IdentityEvidence != "provider" {
		return nil, errors.New("native principal evidence unavailable; add separately and use audited operator reassociation")
	}
	var expected string
	if err := s.db.QueryRow("SELECT identity FROM credentials WHERE account=? ORDER BY ref LIMIT 1", target).Scan(&expected); err != nil {
		return nil, err
	}
	return func(record *auth.Auth) error {
		if record == nil || record.Provider != provider || identity(record) == hash(provider+"\x00credential\x00"+record.ID) || identity(record) != expected {
			if err := s.Audit("account.reauth.rejected", target); err != nil {
				return err
			}
			return errors.New("re-auth native principal/workspace does not match target")
		}
		// A concurrent refresh or reassociation cannot silently change the target
		// principal while this session is waiting for upstream completion.
		return s.transaction("account.reauth.validated", target, func(tx *sql.Tx) error {
			current, exists := s.state.Load().Accounts[target]
			if !exists || current.Provider != provider || current.IdentityEvidence != "provider" || current.ReplacedBy != "" {
				return errors.New("re-auth target changed during session")
			}
			var count, mismatches int
			if err := tx.QueryRow("SELECT count(*),coalesce(sum(identity<>?),0) FROM credentials WHERE account=?", expected, target).Scan(&count, &mismatches); err != nil {
				return err
			}
			if count == 0 || mismatches != 0 {
				return errors.New("re-auth target identity changed during session")
			}
			return nil
		}, func() error { return nil })
	}, nil
}
