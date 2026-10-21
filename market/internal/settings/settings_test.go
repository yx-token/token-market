package settings

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProbeTimeoutValidation(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults should be valid: %v", err)
	}
	cases := []struct {
		name           string
		request, batch int
		ok             bool
	}{
		{"defaults", 180, 30, true},
		{"batch equals one request", 600, 10, true},
		{"request too short", 5, 30, false},
		{"request too long", 3600, 120, false},
		{"batch too short", 10, 0, false},
		{"batch too long", 180, 2000, false},
		{"batch shorter than one request", 600, 5, false},
	}
	for _, c := range cases {
		s := Defaults()
		s.ProbeRequestTimeoutSeconds, s.ProbeBatchTimeoutMinutes = c.request, c.batch
		if err := s.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: validate = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestFreezeSettingsCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, stored, patch string
		hours, days         int
	}{
		{"new default is 24 hours", "", "", 24, 1},
		{"old days are retained", `{"freeze_days":7}`, "", 168, 7},
		{"old zero is explicit", `{"freeze_days":0}`, "", 0, 0},
		{"hours win when both stored", `{"freeze_days":7,"freeze_hours":25}`, "", 25, 2},
		{"zero hours win when both stored", `{"freeze_days":7,"freeze_hours":0}`, "", 0, 0},
		{"patch hours", `{"freeze_days":7}`, `{"freeze_hours":3}`, 3, 1},
		{"patch old days", `{"freeze_hours":3}`, `{"freeze_days":2}`, 48, 2},
		{"patch both prioritizes hours", `{"freeze_days":7}`, `{"freeze_days":5,"freeze_hours":1}`, 1, 1},
		{"unrelated patch retains hours", `{"freeze_hours":25}`, `{"withdraw_min":2}`, 25, 2},
		{"zero disables extra freeze", `{"freeze_hours":25}`, `{"freeze_hours":0}`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := &settingsDB{raw: []byte(tc.stored)}
			var s Settings
			var err error
			if tc.patch == "" {
				s, err = Load(context.Background(), db)
			} else {
				s, err = Update(context.Background(), db, json.RawMessage(tc.patch))
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.FreezeHours != tc.hours || s.FreezeDays != tc.days {
				t.Fatalf("freeze hours/days = %d/%d, want %d/%d", s.FreezeHours, s.FreezeDays, tc.hours, tc.days)
			}
			if tc.patch != "" {
				loaded, err := Load(context.Background(), db)
				if err != nil || loaded.FreezeHours != tc.hours || loaded.FreezeDays != tc.days {
					t.Fatalf("saved settings changed on reload: %+v %v", loaded, err)
				}
			}
		})
	}
}

func TestFreezeSettingsRejectInvalidUpdates(t *testing.T) {
	for _, patch := range []string{
		`{"freeze_hours":-1}`, `{"freeze_days":-1}`,
		`{"freeze_hours":null}`, `{"freeze_days":null}`,
		`{"freeze_hours":1.5}`, `{"freeze_days":1.5}`,
		`{"freeze_hours":9223372036854775807}`, `{"freeze_days":9223372036854775807}`,
		`null`, `[]`,
	} {
		t.Run(patch, func(t *testing.T) {
			db := &settingsDB{}
			if _, err := Update(context.Background(), db, json.RawMessage(patch)); err == nil {
				t.Fatal("expected validation failure")
			}
			if db.saved {
				t.Fatal("invalid settings must not be saved")
			}
		})
	}
	s := Defaults()
	s.FreezeHours = -1
	if err := s.Validate(); err == nil {
		t.Fatal("negative freeze_hours must fail direct validation")
	}
}

func TestCreditUSDToBalanceRateValidation(t *testing.T) {
	if Defaults().CreditUSDToBalanceRate != 1 {
		t.Fatal("activity credit exchange rate should default to 1")
	}
	for _, rate := range []float64{0, -1, 0.000000001, 1000001, 1.123456789, math.NaN(), math.Inf(1), math.Inf(-1)} {
		s := Defaults()
		s.CreditUSDToBalanceRate = rate
		if err := s.Validate(); err == nil {
			t.Errorf("invalid exchange rate accepted: %v", rate)
		}
	}
	for _, rate := range []float64{0.00000001, 0.12345678, 7.2, 1000000} {
		s := Defaults()
		s.CreditUSDToBalanceRate = rate
		if err := s.Validate(); err != nil {
			t.Errorf("valid exchange rate rejected: %v: %v", rate, err)
		}
	}
	db := &settingsDB{}
	if _, err := Update(context.Background(), db, json.RawMessage(`{"credit_usd_to_balance_rate":7.2}`)); err != nil {
		t.Fatal(err)
	}
	s, err := Load(context.Background(), db)
	if err != nil || s.CreditUSDToBalanceRate != 7.2 {
		t.Fatalf("exchange rate not saved: %v %v", s.CreditUSDToBalanceRate, err)
	}
	for _, patch := range []string{
		`{"credit_usd_to_balance_rate":0}`, `{"credit_usd_to_balance_rate":-1}`,
		`{"credit_usd_to_balance_rate":0.000000001}`, `{"credit_usd_to_balance_rate":1000001}`,
		`{"credit_usd_to_balance_rate":1.123456789}`,
	} {
		if _, err := Update(context.Background(), db, json.RawMessage(patch)); err == nil {
			t.Errorf("invalid exchange rate update accepted: %s", patch)
		}
	}
}

type settingsDB struct {
	raw   []byte
	saved bool
}

func (db *settingsDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return settingsRow{raw: db.raw}
}

func (db *settingsDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	db.raw = append([]byte(nil), args[1].([]byte)...)
	db.saved = true
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type settingsRow struct{ raw []byte }

func (r settingsRow) Scan(dest ...any) error {
	if len(r.raw) == 0 {
		return pgx.ErrNoRows
	}
	*dest[0].(*[]byte) = append([]byte(nil), r.raw...)
	return nil
}
