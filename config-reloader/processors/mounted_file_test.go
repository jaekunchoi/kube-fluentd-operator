// Copyright © 2018 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: BSD-2-Clause

package processors

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vmware/kube-fluentd-operator/config-reloader/datasource"

	"github.com/stretchr/testify/assert"
	"github.com/vmware/kube-fluentd-operator/config-reloader/fluentd"
)

func TestMountedFileRemovedAfterProcessing(t *testing.T) {
	s := `
<source>
  @type mounted-file
  path /hello/world
  labels app=spring-mvc
</source>

<match **>
  @type logzio
</match>
	`

	fragment, err := fluentd.ParseString(s)
	assert.Nil(t, err)

	fmt.Printf("Original: %s", fragment)

	ctx := &ProcessorContext{
		Namepsace: "monitoring",
	}

	processor := &mountedFileState{}
	fragment, err = Process(fragment, ctx, processor)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(fragment))
}

func TestMergeMaps(t *testing.T) {
	base := map[string]string{
		"a": "1",
		"b": "2",
	}

	more := map[string]string{
		"a": "-1",
		"z": "26",
	}

	result := mergeMaps(base, more)

	assert.Equal(t, 3, len(result))
	assert.Equal(t, "1", result["a"])
	assert.Equal(t, "2", result["b"])
	assert.Equal(t, "26", result["z"])
}
func TestMountedFileCatchesMissingFile(t *testing.T) {
	missingPath := `
	<source>
	  @type mounted-file
	  labels app=spring-mvc
	</source>

	<match **>
	  @type logzio
	</match
	`
	fragment, err := fluentd.ParseString(missingPath)
	assert.Nil(t, fragment)
	assert.NotNil(t, err, "Must have failed, instead parsed to %+v", fragment)
}

func TestMountedFileCatchesEmptyLabels(t *testing.T) {
	missingPath := `
	<source>
	  @type mounted-file
	  labels
	</source>

	<match **>
	  @type logzio
	</match
	`
	fragment, err := fluentd.ParseString(missingPath)
	assert.Nil(t, fragment)
	assert.NotNil(t, err, "Must have failed, instead parsed to %+v", fragment)
}

func TestMountedFileCatchesMissingLabels(t *testing.T) {
	missingPath := `
	<source>
	  @type mounted-file
	  file /etc/hosts
	</source>

	<match **>
	  @type logzio
	</match
	`
	fragment, err := fluentd.ParseString(missingPath)
	assert.Nil(t, fragment)
	assert.NotNil(t, err, "Must have failed, instead parsed to %+v", fragment)
}

func TestMatches(t *testing.T) {
	spec := &ContainerFile{
		Path: "/var/log/https.log",
	}
	mini := &datasource.MiniContainer{
		PodID:  "123",
		Name:   "container-name",
		Labels: map[string]string{"key": "value"},
	}

	assert.True(t, matches(spec, mini))

	spec.Labels = map[string]string{"_container": "hello"}
	assert.False(t, matches(spec, mini))

	spec.Labels = map[string]string{"_container": mini.Name}
	assert.True(t, matches(spec, mini))

	spec.Labels = map[string]string{"a": "a"}
	assert.False(t, matches(spec, mini))

	spec.Labels = map[string]string{"key": "value"}
	assert.True(t, matches(spec, mini))

	spec.Labels = map[string]string{"key": "value", "_container": "container-name"}
	assert.True(t, matches(spec, mini))

	spec.Labels = map[string]string{"a": "a", "key": "value", "_container": "container-name"}
	assert.False(t, matches(spec, mini))
}

func TestConvertToFragment(t *testing.T) {
	specC1 := &ContainerFile{
		Path:   "/var/log/redis.log",
		Labels: map[string]string{"key": "value", "_container": "container-name"},
		AddedLabels: map[string]string{
			"good": "morning",
			"key":  "new_value", // this will not make it into the final records
		},
	}

	c1 := &datasource.MiniContainer{
		PodID:   "123-id",
		PodName: "123",
		Name:    "container-name",
		Labels:  map[string]string{"key": "value"},
		HostMounts: []*datasource.Mount{
			{
				Path:       "/var/log",
				VolumeName: "logs",
			},
		},
	}

	specC2 := &ContainerFile{
		Path:   "/var/log/nginx.log",
		Labels: map[string]string{"app": "nginx"},
	}
	c2 := &datasource.MiniContainer{
		PodID:   "abc-id",
		PodName: "abc",
		Name:    "nginx",
		Labels:  map[string]string{"app": "nginx"},
		HostMounts: []*datasource.Mount{
			{
				Path:       "/var/log",
				VolumeName: "logs",
			},
			{
				Path:       "/var",
				VolumeName: "var",
			},
		},
	}

	specC3 := &ContainerFile{
		Path:   "/var/log/nginx.log",
		Labels: map[string]string{"app": "nginx-sub"},
	}
	c3 := &datasource.MiniContainer{
		PodID:   "abcd-id",
		PodName: "abcd",
		Name:    "nginx-sub",
		Labels:  map[string]string{"app": "nginx-sub"},
		HostMounts: []*datasource.Mount{
			{
				Path:       "/var/log",
				VolumeName: "logs",
				SubPath:    "files",
			},
		},
	}

	ctx := &ProcessorContext{
		Namepsace:   "monitoring",
		KubeletRoot: "/kubelet-root",
		MiniContainers: []*datasource.MiniContainer{
			c1,
			c2,
			c3,
		},
	}

	state := &mountedFileState{
		BaseProcessorState: BaseProcessorState{
			Context: ctx,
		},
	}

	result := state.convertToFragement(specC1)
	assert.Equal(t, 2, len(result))

	dir := result[0]

	assert.Equal(t, "source", dir.Name)
	assert.Equal(t, "tail", dir.Type())
	assert.Equal(t, "/kubelet-root/pods/123-id/volumes/kubernetes.io~empty-dir/logs/redis.log", dir.Param("path"))
	assert.Equal(t, "kube.monitoring.123.container-name-b3f8f41cab18c93a7c8057277947de0d1d76d1d6", dir.Param("tag"))
	assert.Equal(t, "parse", dir.Nested[0].Name)
	assert.Equal(t, "/var/log/kfotail-b3f8f41cab18c93a7c8057277947de0d1d76d1d6.pos", dir.Param("pos_file"))
	assert.Equal(t, "none", dir.Nested[0].Type())

	mod := result[1]
	assert.Equal(t, "filter", mod.Name)
	assert.Equal(t, "record_modifier", mod.Type())
	assert.True(t, strings.Index(mod.String(), "'good'=>'morning'") > 0)
	assert.True(t, strings.Index(mod.String(), "'key'=>'value'") > 0)
	assert.True(t, strings.Index(mod.String(), "'key'=>'new_value'") < 0)

	result = state.convertToFragement(specC2)
	assert.Equal(t, 2, len(result))

	dir = result[0]

	assert.Equal(t, "source", dir.Name)
	assert.Equal(t, "tail", dir.Type())
	assert.Equal(t, "/kubelet-root/pods/abc-id/volumes/kubernetes.io~empty-dir/logs/nginx.log", dir.Param("path"))
	assert.Equal(t, "kube.monitoring.abc.nginx-82357fcda2cbd45c066d8a538cbf1f3e96b1ce6a", dir.Param("tag"))

	mod = result[1]
	assert.Equal(t, "filter", mod.Name)
	assert.Equal(t, "record_modifier", mod.Type())

	result = state.convertToFragement(specC3)
	assert.Equal(t, 2, len(result))

	dir = result[0]

	assert.Equal(t, "source", dir.Name)
	assert.Equal(t, "tail", dir.Type())
	assert.Equal(t, "/kubelet-root/pods/abcd-id/volumes/kubernetes.io~empty-dir/logs/files/nginx.log", dir.Param("path"))
	assert.Equal(t, "kube.monitoring.abcd.nginx-sub-47c6dc18d51fcc522768361782c12ee10ca66215", dir.Param("tag"))

	mod = result[1]
	assert.Equal(t, "filter", mod.Name)
	assert.Equal(t, "record_modifier", mod.Type())
}

func TestProcessMountedFile(t *testing.T) {
	c1 := &datasource.MiniContainer{
		PodID:       "123-id",
		PodName:     "123",
		Image:       "image-c1",
		ContainerID: "contid-c1",
		Name:        "redis-main",
		Labels:      map[string]string{"app": "redis"},
		HostMounts: []*datasource.Mount{
			{
				Path:       "/var/log",
				VolumeName: "logs",
			},
		},
	}

	c2 := &datasource.MiniContainer{
		PodID:       "abc-id",
		PodName:     "abc",
		Image:       "image-c2",
		ContainerID: "contid-c2",
		Name:        "nginx-main",
		Labels:      map[string]string{"app": "nginx"},
		HostMounts: []*datasource.Mount{
			{
				Path:       "/var/log",
				VolumeName: "logs",
			},
			{
				Path:       "/var",
				VolumeName: "var",
			},
		},
	}

	c3 := &datasource.MiniContainer{
                PodID:       "abc-sub-id",
		PodName:     "abc-sub",
		Image:       "image-c3",
		ContainerID: "contid-c3",
		Name:        "nginx-sub",
		Labels:      map[string]string{"app": "nginx-sub"},
		HostMounts: []*datasource.Mount{
			{
				Path:       "/var/log",
				VolumeName: "logs",
				SubPath:    "files",
			},
		},
	}

	ctx := &ProcessorContext{
		Namepsace:   "monitoring",
		KubeletRoot: "/kubelet-root",
		MiniContainers: []*datasource.MiniContainer{
			c1,
			c2,
			c3,
		},
	}

	state := &mountedFileState{
		BaseProcessorState: BaseProcessorState{
			Context: ctx,
		},
	}

	s := `
	<source>
		@type mounted-file
		path /var/log/redis.log
		labels app=redis
	</source>

	<source>
		@type mounted-file
		path /var/log/nginx.log
		labels app=nginx, _container=nginx-main
	</source>

	<source>
		@type mounted-file
		path /var/log/nginx.log
		labels app=nginx-sub
	</source>

	<match **>
		@type null
	</match>
	`

	input, err := fluentd.ParseString(s)
	assert.Nil(t, err, "Must have parsed, instead got error %+v", err)

	prep, err := Prepare(input, ctx, state)
	assert.Nil(t, err)
	assert.Equal(t, 6, len(prep))
	assert.Equal(t, "/kubelet-root/pods/123-id/volumes/kubernetes.io~empty-dir/logs/redis.log", prep[0].Param("path"))
	assert.Equal(t, "/kubelet-root/pods/abc-id/volumes/kubernetes.io~empty-dir/logs/nginx.log", prep[2].Param("path"))
	assert.Equal(t, "/kubelet-root/pods/abc-sub-id/volumes/kubernetes.io~empty-dir/logs/files/nginx.log", prep[4].Param("path"))

	payload := prep.String()
	assert.True(t, strings.Contains(payload, "'container_image'=>'image-c2'"))
	assert.True(t, strings.Contains(payload, "'container_image'=>'image-c1'"))
	assert.True(t, strings.Contains(payload, "'container_image'=>'image-c3'"))
	assert.True(t, strings.Contains(payload, "record['docker']={'container_id'=>'contid-c1'}"))
	assert.True(t, strings.Contains(payload, "record['docker']={'container_id'=>'contid-c2'}"))
	assert.True(t, strings.Contains(payload, "record['docker']={'container_id'=>'contid-c3'}"))

	main, err := Process(input, ctx, state)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(main))
}
