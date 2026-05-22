// Package datumapi is a thin wrapper around a Datum control-plane API
// session. The on-disk credential is a standard kubeconfig — typically one
// produced by `datumctl auth update-kubeconfig`, which embeds an exec
// credential plugin that calls `datumctl auth get-token` to obtain (and
// refresh) bearer tokens. client-go consumes that natively.
package datumapi

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

const fieldManager = "demo.datum.net"

var (
	GVRSecurityPolicy = schema.GroupVersionResource{
		Group:    "gateway.envoyproxy.io",
		Version:  "v1alpha1",
		Resource: "securitypolicies",
	}
	GVRSecret = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "secrets",
	}
)

type Client struct {
	dyn dynamic.Interface
}

func New(kubeconfigPath string) (*Client, error) {
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("kubeconfig path is required")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return &Client{dyn: dyn}, nil
}

// Smoke proves auth + connectivity by listing SecurityPolicies in the given
// namespace.
func (c *Client) Smoke(ctx context.Context, namespace string) (int, error) {
	if namespace == "" {
		namespace = "default"
	}
	list, err := c.dyn.Resource(GVRSecurityPolicy).Namespace(namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return 0, fmt.Errorf("list securitypolicies in %q: %w", namespace, err)
	}
	return len(list.Items), nil
}

// resourceName is the Secret + SecurityPolicy name for a given tunnel.
func resourceName(tunnel string) string {
	return tunnel + "-basic-auth"
}

// EnableBasicAuth upserts the htpasswd Secret and the SecurityPolicy that
// attaches BasicAuth to the HTTPRoute named `tunnel` in `namespace`. Idempotent
// via server-side apply.
func (c *Client) EnableBasicAuth(ctx context.Context, namespace, tunnel, username, password string) error {
	name := resourceName(tunnel)

	secret := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				"networking.datumapis.com/gateway-sync": "true",
			},
		},
		"type": "Opaque",
		"stringData": map[string]any{
			".htpasswd": htpasswdLine(username, password),
		},
	}}
	if err := c.apply(ctx, GVRSecret, namespace, &secret); err != nil {
		return fmt.Errorf("apply secret: %w", err)
	}

	policy := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.envoyproxy.io/v1alpha1",
		"kind":       "SecurityPolicy",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"targetRefs": []any{
				map[string]any{
					"group": "gateway.networking.k8s.io",
					"kind":  "HTTPRoute",
					"name":  tunnel,
				},
			},
			"basicAuth": map[string]any{
				"users": map[string]any{
					"name": name,
				},
			},
		},
	}}
	if err := c.apply(ctx, GVRSecurityPolicy, namespace, &policy); err != nil {
		return fmt.Errorf("apply securitypolicy: %w", err)
	}
	return nil
}

// DisableBasicAuth deletes the SecurityPolicy and Secret. Missing resources
// are treated as success.
func (c *Client) DisableBasicAuth(ctx context.Context, namespace, tunnel string) error {
	name := resourceName(tunnel)
	if err := c.dyn.Resource(GVRSecurityPolicy).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete securitypolicy %q: %w", name, err)
	}
	if err := c.dyn.Resource(GVRSecret).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete secret %q: %w", name, err)
	}
	return nil
}

func (c *Client) apply(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured) error {
	data, err := json.Marshal(obj.Object)
	if err != nil {
		return err
	}
	_, err = c.dyn.Resource(gvr).Namespace(namespace).Patch(
		ctx,
		obj.GetName(),
		types.ApplyPatchType,
		data,
		metav1.PatchOptions{FieldManager: fieldManager, Force: ptrBool(true)},
	)
	return err
}

func ptrBool(b bool) *bool { return &b }

// htpasswdLine returns "<user>:{SHA}<base64(sha1(password))>", the only
// algorithm Envoy Gateway's BasicAuth accepts today.
func htpasswdLine(user, password string) string {
	sum := sha1.Sum([]byte(password))
	return user + ":{SHA}" + base64.StdEncoding.EncodeToString(sum[:])
}
