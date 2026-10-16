package identity

import (
	"errors"
	"reflect"
	"testing"
)

func TestDeploymentIdentityIsOrderIndependent(t *testing.T) {
	first := compute(t, "key", components("cpu", "A", "drive", "B"))
	second := compute(t, "key", components("drive", "B", "cpu", "A"))
	if !reflect.DeepEqual(first, second) {
		t.Fatal("equal typed component sets produced different deployment identities")
	}
}

func TestSameMaterialDiffersAcrossDeploymentKeys(t *testing.T) {
	first := compute(t, "first-key", components("drive", "same"))
	second := compute(t, "second-key", components("drive", "same"))
	if first.ComponentPseudonyms[0].Digest == second.ComponentPseudonyms[0].Digest {
		t.Fatal("component pseudonym did not change with deployment key")
	}
	if first.SystemHMAC == second.SystemHMAC {
		t.Fatal("system HMAC did not change with deployment key")
	}
}

func TestDeploymentKeyIDIdentifiesExactKey(t *testing.T) {
	first, err := DeploymentKeyID([]byte("first-key"))
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := DeploymentKeyID([]byte("first-key"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := DeploymentKeyID([]byte("second-key"))
	if err != nil {
		t.Fatal(err)
	}
	if first != repeat || first == second {
		t.Fatal("fingerprint key identifier is not stable and key-specific")
	}
}

func TestPartSwapIsAttributable(t *testing.T) {
	first := compute(t, "key", components("cpu", "A", "drive", "B"))
	second := compute(t, "key", components("cpu", "A", "drive", "C"))
	if pseudonym(first, "cpu") != pseudonym(second, "cpu") {
		t.Fatal("unchanged CPU pseudonym changed")
	}
	if pseudonym(first, "drive") == pseudonym(second, "drive") {
		t.Fatal("replacement drive pseudonym did not change")
	}
	if first.SystemHMAC == second.SystemHMAC {
		t.Fatal("part swap did not change system HMAC")
	}
}

func TestDuplicateIdenticalPartsRemainDistinctRecords(t *testing.T) {
	one := compute(t, "key", components("memory", "A"))
	two := compute(t, "key", components("memory", "A", "memory", "A"))
	if len(two.ComponentPseudonyms) != 2 || two.ComponentPseudonyms[0] != two.ComponentPseudonyms[1] {
		t.Fatal("identical parts must remain duplicate pseudonym records")
	}
	if one.SystemHMAC == two.SystemHMAC {
		t.Fatal("duplicate part count did not change system HMAC")
	}
}

func TestComputeRejectsEmptyKey(t *testing.T) {
	_, err := Compute(nil, components("cpu", "A"))
	if !errors.Is(err, ErrEmptyDeploymentKey) {
		t.Fatalf("error = %v, want ErrEmptyDeploymentKey", err)
	}
}

func TestMissingIdentifierMustBeOmitted(t *testing.T) {
	_, err := Compute([]byte("key"), []ComponentMaterial{{Domain: "drive"}})
	if !errors.Is(err, ErrMissingIdentifier) {
		t.Fatalf("error = %v, want ErrMissingIdentifier", err)
	}
	withoutMissing := compute(t, "key", nil)
	if len(withoutMissing.ComponentPseudonyms) != 0 {
		t.Fatal("omitted identifier created a pseudonym")
	}
}

func compute(t *testing.T, key string, values []ComponentMaterial) DeploymentIdentity {
	t.Helper()
	result, err := Compute([]byte(key), values)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func components(values ...string) []ComponentMaterial {
	result := make([]ComponentMaterial, 0, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		result = append(result, ComponentMaterial{Domain: values[index], Material: []byte(values[index+1])})
	}
	return result
}

func pseudonym(result DeploymentIdentity, domain string) Digest {
	for _, component := range result.ComponentPseudonyms {
		if component.Domain == domain {
			return component.Digest
		}
	}
	return Digest{}
}
