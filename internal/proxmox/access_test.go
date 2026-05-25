package proxmox

import (
	"encoding/json"
	"testing"
)

// PVE's POST /access/users/{id}/token/{name} returns privsep/expire inside
// `info` as JSON strings ("0"), not numbers. FlexInt must absorb both so the
// add-user flow can read back the freshly-minted secret.
func TestTokenCreated_DecodesStringEncodedInfo(t *testing.T) {
	// Body shape after PostForm unwraps the `data` envelope.
	raw := `{
		"full-tokenid": "bob@pve!murmur",
		"value": "deadbeef-0000-1111-2222-333344445555",
		"info": {"privsep": "0", "expire": "0"}
	}`
	var got TokenCreated
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Value != "deadbeef-0000-1111-2222-333344445555" {
		t.Errorf("Value = %q", got.Value)
	}
	if got.FullTokenID != "bob@pve!murmur" {
		t.Errorf("FullTokenID = %q", got.FullTokenID)
	}
	if got.Info.PrivSep != 0 || got.Info.Expire != 0 {
		t.Errorf("info ints = privsep:%d expire:%d", got.Info.PrivSep, got.Info.Expire)
	}
}

func TestFlexInt_AcceptsNumberStringNull(t *testing.T) {
	cases := map[string]FlexInt{
		`5`:    5,
		`"7"`:  7,
		`0`:    0,
		`"0"`:  0,
		`""`:   0,
		`null`: 0,
	}
	for in, want := range cases {
		var n FlexInt
		if err := json.Unmarshal([]byte(in), &n); err != nil {
			t.Errorf("%s: unexpected err %v", in, err)
			continue
		}
		if n != want {
			t.Errorf("%s: got %d want %d", in, n, want)
		}
	}
}
