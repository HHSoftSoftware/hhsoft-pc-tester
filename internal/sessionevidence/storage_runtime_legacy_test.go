package sessionevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// These literals were captured by executing the unchanged v4/v5 readers.
// Fixture composition below is exactly the independently validated baseline.
func TestStorageReaderPreservesRuntimeLegacyCanonicalHashes(t *testing.T) {
	type fixture struct {
		name     string
		journal  Journal
		bindings []ResultBinding
	}
	cases := []fixture{}
	add := func(name string, journal Journal, bindings []ResultBinding) {
		cases = append(cases, fixture{name, journal, bindings})
	}
	j := shortJournal()
	add("v5_all_off", j, shortBindings(j))
	j, rows := hotJournal(t)
	add("v5_cpu_v2", j, rows)
	j, rows = scalarCPUHotBinding(t)
	add("v5_cpu_v3", j, rows)
	j, rows = memoryHotBinding(t, false)
	add("v5_memory_v2", j, rows)
	j, rows = memoryHotBinding(t, true)
	add("v5_memory_v3", j, rows)
	j, rows = keyboardBindingFixture(t)
	add("v5_keyboard", j, rows)
	j, rows = pointerBindingFixture(t)
	add("v5_pointer_v2", j, rows)
	j = shortJournal()
	j, rows = historicalPointerSession(j, shortBindings(j))
	add("v4_all_off", j, rows)
	j, rows = hotJournal(t)
	j, rows = historicalPointerSession(j, rows)
	add("v4_cpu_v2", j, rows)
	j, rows = memoryHotBinding(t, false)
	j, rows = historicalPointerSession(j, rows)
	add("v4_memory_v2", j, rows)
	j, rows = pointerBindingFixture(t)
	for index := range rows {
		if rows[index].ID == "input.pointer.quick" {
			rows[index] = historicalPointerBinding(t, rows[index])
		}
	}
	j, rows = historicalPointerSession(j, rows)
	add("v4_pointer_v1", j, rows)
	if len(cases) != 11 {
		t.Fatal("case inventory changed")
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if ValidateBindings(item.journal, item.bindings) != nil {
				t.Fatal("baseline binding invalid")
			}
			journal, err := json.Marshal(item.journal)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(journal); err != nil {
				t.Fatal("baseline journal invalid", err)
			}
			integrity, err := json.Marshal(IntegrityEvidence{Schema: IntegritySchema, Status: "pass", PlanID: item.journal.PlanID, PlannedHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), FinalHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), PlannedComponentCount: 3, FinalComponentCount: 3, Session: &item.journal})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeIntegrity(integrity); err != nil {
				t.Fatal("baseline integrity invalid", err)
			}
			bindings, err := json.Marshal(item.bindings)
			if err != nil {
				t.Fatal(err)
			}
			digest := func(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
			out := struct {
				Name            string `json:"name"`
				Schema          string `json:"schema"`
				Workers         int    `json:"workers"`
				JournalBytes    int    `json:"journal_bytes"`
				JournalSHA256   string `json:"journal_sha256"`
				IntegrityBytes  int    `json:"integrity_bytes"`
				IntegritySHA256 string `json:"integrity_sha256"`
				BindingsBytes   int    `json:"bindings_bytes"`
				BindingsSHA256  string `json:"bindings_sha256"`
			}{item.name, item.journal.Schema, len(item.journal.Workers), len(journal), digest(journal), len(integrity), digest(integrity), len(bindings), digest(bindings)}
			var expected []struct {
				Name            string `json:"name"`
				Schema          string `json:"schema"`
				Workers         int    `json:"workers"`
				JournalBytes    int    `json:"journal_bytes"`
				JournalSHA256   string `json:"journal_sha256"`
				IntegrityBytes  int    `json:"integrity_bytes"`
				IntegritySHA256 string `json:"integrity_sha256"`
				BindingsBytes   int    `json:"bindings_bytes"`
				BindingsSHA256  string `json:"bindings_sha256"`
			}
			if json.Unmarshal([]byte(`[{"bindings_bytes":2767,"bindings_sha256":"87a02f1b0d4b291099a65eb429c45492b45afce6e01cb061b82c3e1aee9bc1c7","integrity_bytes":3850,"integrity_sha256":"01f2b4ca6a01e03993beff3781387756fd0048f05b7425b7dff44746ed37861f","journal_bytes":3433,"journal_sha256":"3ae9870c788380a0137c7d223b1e5f08378fbec9ab3f87e0a4ceb864110e594e","name":"v5_all_off","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":3311,"bindings_sha256":"a330d052ddb10aa283482b241f6c9b8599ebd2f607d29fd112882ee76c2d292a","integrity_bytes":4450,"integrity_sha256":"c1f3b3344d613d3756b9492a3e50641331bac560f2be2ef0880f44d93f358331","journal_bytes":4033,"journal_sha256":"c65b4a61892fb919eb03b48229ab7a43734a0c9f15bd3a6d691b80f1b58b3e7d","name":"v5_cpu_v2","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":3647,"bindings_sha256":"10c8902d4444f6c3358fee836f0e50cb6470bcf4a643894ea3c14a9a3383c70f","integrity_bytes":4450,"integrity_sha256":"c03e9db59481803f29d7e1e925a2a243eb325c6dbde8a8a949e1dbc7974d2c55","journal_bytes":4033,"journal_sha256":"998c94e4ae5bcd4e62c9af1896d333ade102b85a3b64a17780c76f54bdd140ba","name":"v5_cpu_v3","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":3479,"bindings_sha256":"1edcf19561535569d6b58a1001bc2df5a48633d12fd8821fe33c09670cfbf412","integrity_bytes":4459,"integrity_sha256":"11ac261a2c81c8e7df356f87acee9c5dd5174db9893da950deaeddf698eef6d3","journal_bytes":4042,"journal_sha256":"922a82e9ac3b2e084a360ede52954cf164cdb85e5560093ba023af329ceb546c","name":"v5_memory_v2","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":3539,"bindings_sha256":"4365c007bf3767c2815954c8dbfa8de2d76d5623fbedf404c934f12edbb39bd7","integrity_bytes":4459,"integrity_sha256":"0f9202de6574db070416c67db70406495ce6b9b46a92620f718da372adc23feb","journal_bytes":4042,"journal_sha256":"77f5de374848b25acd39ef2e22409091b4b29b2e99029e53e0ac315e2e6c49a6","name":"v5_memory_v3","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":3972,"bindings_sha256":"eab7b5076e3dec1fe59ac13f5588cde42f9876d191a5ed5c8568460212e0d7ad","integrity_bytes":4104,"integrity_sha256":"13d5d9d10149f11e1dd9c2ff7e31dd8be069cc5b51b1d5be986db6be96f16896","journal_bytes":3687,"journal_sha256":"c5a514df6b72c7b765cb582fbfad6a0b630b17c87c0fde0fb3915e6b70d4e78e","name":"v5_keyboard","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":3532,"bindings_sha256":"1880235368e506969dad31d4a99b6a630657e235e247985e0d5c0efe7a2cd92d","integrity_bytes":4103,"integrity_sha256":"90a9229cb487f4478a9f63c623011e5800d5d89793cacc52c76b156e28dff56e","journal_bytes":3686,"journal_sha256":"980174750c07f23b2b522da0de355786b3a4a5df766671b26fa21efc39496834","name":"v5_pointer_v2","schema":"hhsoft.pc-tester.quick-session/v5","workers":11},{"bindings_bytes":2514,"bindings_sha256":"7914de30873fe893711c2b7f66418e5857d5a6836917485713c52cd705abda7c","integrity_bytes":3775,"integrity_sha256":"b1c07e15cc432c8ee585e6b835cb390f5f8d637c783b2e48f4a340b7fe86a451","journal_bytes":3358,"journal_sha256":"db25b945b530a18d626a8b65be28edb098e36a779b4602275eeef8693986c519","name":"v4_all_off","schema":"hhsoft.pc-tester.quick-session/v4","workers":10},{"bindings_bytes":3058,"bindings_sha256":"7c55c6e388d0fac7803dbac7a5a0c726a37f9b311ec8fa153c1bdc299dc67e73","integrity_bytes":4375,"integrity_sha256":"874397d42b20e021ad07bcafbdf0142d4aa87120e2b0f84d84d6a0f2677969c3","journal_bytes":3958,"journal_sha256":"d0d51233678b5e6eb499d4b42cce3df1636f505d20c78d3a1aa8dcfca01ea3c8","name":"v4_cpu_v2","schema":"hhsoft.pc-tester.quick-session/v4","workers":10},{"bindings_bytes":3226,"bindings_sha256":"3fccf930d637db59371657cfa60ac4eb0fe2f3379485363f689748373f5ab535","integrity_bytes":4384,"integrity_sha256":"aefe25848b233149760ca701a5c249948932220f10aa5233b24e1879d4c6267b","journal_bytes":3967,"journal_sha256":"5c36f39d09b8a112cdef17ac3c499b6f33536d3700e4b43a809fc89daf97aaec","name":"v4_memory_v2","schema":"hhsoft.pc-tester.quick-session/v4","workers":10},{"bindings_bytes":3087,"bindings_sha256":"e96dbc37aa0040f098e7442381f9ba5df50aad9f2c9e2c41751c607b748d7c60","integrity_bytes":4028,"integrity_sha256":"65886eee79248432f32aa9df8985316c0f36dede62bdedead3842f0ba006e54f","journal_bytes":3611,"journal_sha256":"350cac4e319ef6d53baabdcc4f8122b1835bea9135c9f20caea8930b5f50ff45","name":"v4_pointer_v1","schema":"hhsoft.pc-tester.quick-session/v4","workers":10}]`), &expected) != nil {
				t.Fatal("literal baseline invalid")
			}
			found := false
			for _, want := range expected {
				if want.Name == item.name {
					found = true
					if want != out {
						t.Fatal("legacy canonical bytes or worker inventory changed", item.name)
					}
				}
			}
			if !found {
				t.Fatal("unaccounted legacy fixture")
			}

		})
	}
}
