package app

import "testing"

func TestSessionThermalClaimSelectionCannotLoseNewClaims(t *testing.T) {
	for _, encoded := range []string{
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v4"}}`,
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v4","schema":"hhsoft.pc-tester.quick-session/v3"}}`,
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v4"},"session":null}`,
		`{"Seſsion":{"ſchema":"HHSOFT.PC-TESTER.QUICK-SESSION/V4"}}`,
		`{"session":{"schem\u0061":"hhsoft.pc-tester.quick-session/v4"}}`,
		`{"test_results":{"evidence":[{"schema":"hhsoft.pc-tester.run-thermal/v1"}]}}`,
		`{"test_results":[{"evidence":{"session":{"thermal":null}}}]}`,
		`{"test_results":{"evidence":{"session":{"Thermal":{}}}},"test_results":null}`,
		`{"session":{"therm\u0061l":false}}`,
		`{"session":{"local_thermal_sourceſ":[]}}`,
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v4"}`,
		`{"historical":true} {"schema":"hhsoft.pc-tester.run-thermal/v1"}`,
	} {
		if !HasSessionThermalClaim([]byte(encoded)) {
			t.Fatal("new or malformed claim did not select strict validation")
		}
	}
}

func TestSessionThermalClaimSelectionPreservesHistoricalSchemas(t *testing.T) {
	for _, encoded := range []string{
		`{"schema_version":1}`,
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v1"}}`,
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v2"}}`,
		`{"session":{"schema":"hhsoft.pc-tester.quick-session/v3","monitor":{"schema":"hhsoft.pc-tester.safety-monitor/v1"}}}`,
		`{"schema":"hhsoft.pc-tester.cpu-quick/v2","safety":{"thermal_coverage":"unavailable","thermal_limit_millicelsius":90000}}`,
	} {
		if HasSessionThermalClaim([]byte(encoded)) {
			t.Fatal("historical evidence acquired a run-wide thermal claim")
		}
	}
	if HasSessionThermalClaim(nil) || HasSessionThermalClaim([]byte{}) {
		t.Fatal("absent historical evidence acquired a thermal claim")
	}
}
