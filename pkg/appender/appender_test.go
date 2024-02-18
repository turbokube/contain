package appender_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	. "github.com/onsi/gomega"
	"github.com/turbokube/contain/pkg/appender"
	"github.com/turbokube/contain/pkg/localdir"
	schema "github.com/turbokube/contain/pkg/schema/v1"
	"github.com/turbokube/contain/pkg/testcases"
)

func TestAppender(t *testing.T) {
	RegisterTestingT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := testcases.NewTestregistry(ctx)

	if err := r.Start(); err != nil {
		t.Fatalf("testregistry start %v", err)
	}

	base, err := name.ParseReference(
		// the amd64 manifest in contain-test/baseimage-multiarch1:noattest
		fmt.Sprintf("%s/contain-test/baseimage-multiarch1:noattest@sha256:88b8e36da2fe3947b813bd52473319c3fb2e7637692ff4c499fa8bd878241852", r.Host),
		r.Config.CraneOptions.Name...,
	)
	if err != nil {
		t.Fatal(err)
	}

	tag, err := name.ParseReference(
		fmt.Sprintf("%s/contain-test/append-test:1", r.Host),
		r.Config.CraneOptions.Name...,
	)
	if err != nil {
		t.Fatal(err)
	}

	a, err := appender.New(base.(name.Digest), &r.Config, tag.(name.Tag))
	if err != nil {
		t.Fatal(err)
	}

	layer1, err := localdir.Layer(map[string][]byte{
		"test.txt": []byte("test"),
	}, schema.LayerAttributes{})
	if err != nil {
		t.Fatal(err)
	}

	layer2, err := localdir.Layer(map[string][]byte{
		"2": []byte("2"),
	}, schema.LayerAttributes{})
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Append(layer1, layer2)
	if err != nil {
		t.Error(err)
	}

	tagged, err := remote.Get(tag, r.Config.CraneOptions.Remote...)
	if err != nil {
		t.Errorf("%s wasn't pushed? %v", tag, err)
	}
	Expect(tagged.MediaType).To(Equal(types.OCIManifestSchema1))
	Expect(tagged.Digest.String()).To(Equal("sha256:c08efcef72c451bc8b7c5b9cba39f1e18bfa6bd030df188ffbfefaebcea0203f"))
	// what's this digest for?
	Expect(tagged.RawManifest()).To(ContainSubstring("sha256:6e18a873d324ec1e1f8a03f35fa4e29b46a7389ce2a7439342f01d6c402bf477"))

	pm, err := result.Pushed.Add.MediaType()
	Expect(err).NotTo(HaveOccurred())
	Expect(pm).To(Equal(types.OCIManifestSchema1))
	pd, err := result.Pushed.Add.Digest()
	Expect(err).NotTo(HaveOccurred())
	Expect(pd.String()).To(Equal("sha256:c08efcef72c451bc8b7c5b9cba39f1e18bfa6bd030df188ffbfefaebcea0203f"))
	ps, err := result.Pushed.Add.Size()
	Expect(err).NotTo(HaveOccurred())
	Expect(ps).To(BeEquivalentTo(719))
	Expect(result.Pushed.Platform.Architecture).To(Equal("amd64"))
	Expect(result.Pushed.Platform.OS).To(Equal("linux"))

	Expect(result.Hash.String()).To(Equal("sha256:c08efcef72c451bc8b7c5b9cba39f1e18bfa6bd030df188ffbfefaebcea0203f"))
	Expect(result.AddedManifestLayers).To(HaveLen(2))
	added1 := result.AddedManifestLayers[0]
	Expect(added1.MediaType).To(Equal(types.DockerLayer))
	Expect(added1.Digest.String()).To(Equal("sha256:72b763668602c1aaab0c817a9478a823ce68e3de59239dad3561c17452dda66b"))
	added2 := result.AddedManifestLayers[1]
	Expect(added2.MediaType).To(Equal(types.DockerLayer))
	Expect(added2.Digest.String()).To(Equal("sha256:325d1bfeb1d4ae147119c509b873f23c0fdcfd2c829b23ed529089f4e1bb5914"))
}

func TestAppenderResultLayer(t *testing.T) {
	RegisterTestingT(t)

	r := appender.AppendResultLayer{
		MediaType: types.OCILayerZStd,
		Size:      123,
		Digest:    testcases.NewMockHash(""),
	}
	d := r.Descriptor()

	// test that the struct defintion is a subset of v1.Descriptor
	// and that all subset field values are included
	superset := reflect.ValueOf(d)
	subset := reflect.ValueOf(r)
	for i := 0; i < subset.NumField(); i++ {
		field := subset.Type().Field(i)
		if !superset.FieldByName(field.Name).IsValid() {
			t.Errorf("Field %s is not present in LargeStruct\n", field.Name)
		} else {
			Expect(subset.FieldByName(field.Name).Interface()).To(Equal(superset.FieldByName(field.Name).Interface()))
		}
	}

	// test that fields required for JSON marshalling are present
	j1 := bytes.NewBuffer([]byte{})
	json.NewEncoder(j1).Encode(r)
	j2 := bytes.NewBuffer([]byte{})
	json.NewEncoder(j2).Encode(d)
	Expect(j1).To(Equal(j2))
}
