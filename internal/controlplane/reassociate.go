package controlplane

import (
	"database/sql"
	"errors"
	"strings"
)

// ReassociateCredential is an explicit admin assertion, never an email heuristic.
// Only an unused fallback-identity import can be attached to an existing account.
// Existing bindings, ownership, history and counters are not moved or merged.
func (s *Store) ReassociateCredential(target, source, confirmation string) error {
	if confirmation != target || target == source {
		return errors.New("confirm the existing logical account ID")
	}
	return s.mutate("account.reassociate", target+":"+source, func(tx *sql.Tx) error {
		st := s.state.Load()
		old, oldOK := st.Accounts[target]
		fresh, freshOK := st.Accounts[source]
		if !oldOK || !freshOK || old.ReplacedBy != "" || fresh.ReplacedBy != "" || old.Provider != fresh.Provider {
			return errors.New("accounts must be unreplaced members of the same provider")
		}
		if !fresh.Paused || s.InFlight(source) != 0 || fresh.Requests != 0 || fresh.WarmEnabled || fresh.IdentityEvidence != "credential_reference" {
			return errors.New("replacement must be paused, idle, unused and credential-reference identified; verified principals cannot be relinked")
		}
		for _, key := range st.Keys {
			if contains(key.Bindings[fresh.Provider].Accounts, source) {
				return errors.New("replacement already has API-key scope")
			}
		}
		for _, pool := range st.Pools {
			if pool.ID != "default-"+fresh.Provider && contains(pool.Accounts, source) {
				return errors.New("replacement already belongs to a configured pool")
			}
		}
		var owners int
		if err := tx.QueryRow("SELECT count(*) FROM affinity WHERE account=?", source).Scan(&owners); err != nil {
			return err
		}
		if owners != 0 {
			return errors.New("replacement already owns affinity")
		}
		rows, err := tx.Query("SELECT ref,identity FROM credentials WHERE account=?", source)
		if err != nil {
			return err
		}
		var refs []string
		for rows.Next() {
			var ref, fingerprint string
			if err = rows.Scan(&ref, &fingerprint); err != nil {
				break
			}
			credential, matches := strings.CutPrefix(ref, fresh.Provider+":")
			if !matches || fingerprint != hash(fresh.Provider+"\x00credential\x00"+credential) {
				err = errors.New("replacement has provider identity evidence")
				break
			}
			refs = append(refs, ref)
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			return err
		}
		if len(refs) == 0 {
			return errors.New("replacement credential unavailable")
		}
		var identity string
		if err = tx.QueryRow("SELECT identity FROM credentials WHERE account=? ORDER BY ref LIMIT 1", target).Scan(&identity); err != nil {
			return err
		}
		for _, ref := range refs {
			if _, err = tx.Exec("UPDATE credentials SET account=?,identity=? WHERE ref=?", target, identity, ref); err != nil {
				return err
			}
		}
		fresh.ReplacedBy = target
		fresh.Health = "replaced"
		if err = put(tx, "account", source, fresh); err != nil {
			return err
		}
		if old.IdentityEvidence == "credential_reference" {
			old.IdentityEvidence = "operator_confirmed"
		}
		if err = put(tx, "account", target, old); err != nil {
			return err
		}
		pool := st.Pools["default-"+fresh.Provider]
		members := make([]string, 0, len(pool.Accounts))
		for _, id := range pool.Accounts {
			if id != source {
				members = append(members, id)
			}
		}
		pool.Accounts = members
		return put(tx, "pool", pool.ID, pool)
	})
}
