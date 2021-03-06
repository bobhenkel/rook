/*
Copyright 2016 The Rook Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package api

import (
	"fmt"

	"github.com/rook/rook/pkg/model"
	"github.com/rook/rook/pkg/operator/k8sutil"
	opmon "github.com/rook/rook/pkg/operator/mon"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/pkg/api/v1"
	extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

const (
	DeploymentName = "rook-api"
)

type Cluster struct {
	context   *k8sutil.Context
	Name      string
	Namespace string
	Version   string
	Replicas  int32
}

func New(context *k8sutil.Context, name, namespace, version string) *Cluster {
	return &Cluster{
		context:   context,
		Name:      name,
		Namespace: namespace,
		Version:   version,
		Replicas:  1,
	}
}

func (c *Cluster) Start() error {
	logger.Infof("starting the Rook api")

	// start the service
	err := c.startService()
	if err != nil {
		return fmt.Errorf("failed to start api service. %+v", err)
	}

	// start the deployment
	deployment := c.makeDeployment()
	_, err = c.context.Clientset.ExtensionsV1beta1().Deployments(c.Namespace).Create(deployment)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create api deployment. %+v", err)
		}
		logger.Infof("api deployment already exists")
	} else {
		logger.Infof("api deployment started")
	}

	return nil
}

func (c *Cluster) makeDeployment() *extensions.Deployment {
	deployment := &extensions.Deployment{}
	deployment.Name = DeploymentName
	deployment.Namespace = c.Namespace

	podSpec := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:        DeploymentName,
			Labels:      c.getLabels(),
			Annotations: map[string]string{},
		},
		Spec: v1.PodSpec{
			Containers:    []v1.Container{c.apiContainer()},
			RestartPolicy: v1.RestartPolicyAlways,
			Volumes: []v1.Volume{
				{Name: k8sutil.DataDirVolume, VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}},
			},
		},
	}

	deployment.Spec = extensions.DeploymentSpec{Template: podSpec, Replicas: &c.Replicas}

	return deployment
}

func (c *Cluster) apiContainer() v1.Container {

	command := fmt.Sprintf("/usr/bin/rookd api --config-dir=%s --port=%d ", k8sutil.DataDir, model.Port)
	return v1.Container{
		// TODO: fix "sleep 5".
		// Without waiting some time, there is highly probable flakes in network setup.
		Command: []string{"/bin/sh", "-c", fmt.Sprintf("sleep 5; %s", command)},
		Name:    DeploymentName,
		Image:   k8sutil.MakeRookImage(c.Version),
		VolumeMounts: []v1.VolumeMount{
			{Name: k8sutil.DataDirVolume, MountPath: k8sutil.DataDir},
		},
		Env: []v1.EnvVar{
			v1.EnvVar{Name: "ROOKD_VERSION_TAG", Value: c.Version},
			k8sutil.NamespaceEnvVar(),
			k8sutil.RepoPrefixEnvVar(),
			opmon.MonSecretEnvVar(),
			opmon.AdminSecretEnvVar(),
			opmon.MonEndpointEnvVar(),
			opmon.ClusterNameEnvVar(c.Name),
		},
	}
}

func (c *Cluster) startService() error {
	labels := c.getLabels()
	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName,
			Namespace: c.Namespace,
			Labels:    labels,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       DeploymentName,
					Port:       model.Port,
					TargetPort: intstr.FromInt(int(model.Port)),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Selector: labels,
		},
	}

	s, err := c.context.Clientset.CoreV1().Services(c.Namespace).Create(s)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create api service. %+v", err)
		}
		logger.Infof("api service already running")
		return nil
	}

	logger.Infof("API service running at %s:%d", s.Spec.ClusterIP, model.Port)
	return nil
}

func (c *Cluster) getLabels() map[string]string {
	return map[string]string{
		k8sutil.AppAttr:     DeploymentName,
		k8sutil.ClusterAttr: c.Namespace,
	}
}
