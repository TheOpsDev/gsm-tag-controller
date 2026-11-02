/*
Copyright 2026.

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

package gcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrTagNotFound indicates a tag key or value short name does not exist under
// the configured tag parent. The controller only binds pre-existing tags;
// it surfaces this in status and keeps requeuing, since tags can be created
// in GCP out-of-band.
var ErrTagNotFound = errors.New("tag key or value not found under the configured GCP tag parent")

// TagBinder reconciles Resource Manager tag bindings on GSM secrets.
type TagBinder interface {
	// SyncTags performs a full sync of tag bindings on the named secret:
	// bindings are added for entries in desired, and parent-scoped bindings
	// absent from desired are removed. It returns the tags bound after the
	// sync.
	SyncTags(ctx context.Context, secretName string, desired map[string]string) (map[string]string, error)

	// RemoveTags removes all parent-scoped tag bindings from the named
	// secret. Used when a GSMSecretTag with deletionPolicy Delete is deleted.
	RemoveTags(ctx context.Context, secretName string) error
}

// Client implements TagBinder against the GCP Resource Manager API using
// Application Default Credentials (Workload Identity in-cluster).
//
// Only bindings whose tag keys live under the configured tag parent (a
// numeric organization ID or a project ID) are managed; bindings from other
// parents are left untouched.
type Client struct {
	tagParent   string
	projectID   string
	tagValues   *resourcemanager.TagValuesClient
	tagBindings *resourcemanager.TagBindingsClient
}

// NewClient returns a TagBinder scoped to the given tag parent and project.
func NewClient(ctx context.Context, tagParent, projectID string) (*Client, error) {
	tagValues, err := resourcemanager.NewTagValuesClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating tag values client: %w", err)
	}
	tagBindings, err := resourcemanager.NewTagBindingsClient(ctx)
	if err != nil {
		_ = tagValues.Close()
		return nil, fmt.Errorf("creating tag bindings client: %w", err)
	}
	return &Client{
		tagParent:   tagParent,
		projectID:   projectID,
		tagValues:   tagValues,
		tagBindings: tagBindings,
	}, nil
}

// Close releases the underlying API connections.
func (c *Client) Close() error {
	return errors.Join(c.tagValues.Close(), c.tagBindings.Close())
}

// secretResourceName builds the full resource name required by the tag
// bindings API.
func (c *Client) secretResourceName(secretName string) string {
	return fmt.Sprintf("//secretmanager.googleapis.com/projects/%s/secrets/%s", c.projectID, secretName)
}

func (c *Client) SyncTags(ctx context.Context, secretName string, desired map[string]string) (map[string]string, error) {
	// Resolve every desired short-name pair to its tagValues/{id} resource
	// name up front, so a missing tag fails the sync before any mutation.
	desiredValueIDs := make(map[string]string, len(desired))
	for key, value := range desired {
		id, err := c.resolveTagValue(ctx, key, value)
		if err != nil {
			return nil, err
		}
		desiredValueIDs[key] = id
	}

	parent := c.secretResourceName(secretName)
	existing, err := c.listBindings(ctx, parent)
	if err != nil {
		return nil, err
	}

	// Full sync: remove parent-scoped bindings not in desired first, since a
	// resource can hold only one value per tag key.
	for _, binding := range existing {
		key, value, ok := c.parseNamespacedValue(binding.TagValueNamespacedName)
		if !ok {
			continue
		}
		if want, found := desired[key]; found && want == value {
			continue
		}
		if err := c.deleteBinding(ctx, binding.Name); err != nil {
			return nil, fmt.Errorf("removing binding %s=%s from %s: %w", key, value, secretName, err)
		}
	}

	bound := currentTags(existing, c.tagParent)
	for key, value := range desired {
		if bound[key] == value {
			continue
		}
		op, err := c.tagBindings.CreateTagBinding(ctx, &resourcemanagerpb.CreateTagBindingRequest{
			TagBinding: &resourcemanagerpb.TagBinding{
				Parent:   parent,
				TagValue: desiredValueIDs[key],
			},
		})
		if err != nil {
			return nil, fmt.Errorf("binding %s=%s to %s: %w", key, value, secretName, err)
		}
		if _, err := op.Wait(ctx); err != nil {
			return nil, fmt.Errorf("binding %s=%s to %s: %w", key, value, secretName, err)
		}
	}

	result := make(map[string]string, len(desired))
	maps.Copy(result, desired)
	return result, nil
}

func (c *Client) RemoveTags(ctx context.Context, secretName string) error {
	existing, err := c.listBindings(ctx, c.secretResourceName(secretName))
	if err != nil {
		return err
	}
	for _, binding := range existing {
		key, value, ok := c.parseNamespacedValue(binding.TagValueNamespacedName)
		if !ok {
			continue
		}
		if err := c.deleteBinding(ctx, binding.Name); err != nil {
			return fmt.Errorf("removing binding %s=%s from %s: %w", key, value, secretName, err)
		}
	}
	return nil
}

// resolveTagValue looks up the tagValues/{id} resource name for a short-name
// pair under the tag parent.
func (c *Client) resolveTagValue(ctx context.Context, key, value string) (string, error) {
	tv, err := c.tagValues.GetNamespacedTagValue(ctx, &resourcemanagerpb.GetNamespacedTagValueRequest{
		Name: fmt.Sprintf("%s/%s/%s", c.tagParent, key, value),
	})
	if status.Code(err) == codes.NotFound || status.Code(err) == codes.PermissionDenied {
		// The API returns PermissionDenied for nonexistent tags to avoid
		// leaking their existence; treat both as not found.
		return "", fmt.Errorf("tag %s=%s under parent %s: %w", key, value, c.tagParent, ErrTagNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("resolving tag %s=%s: %w", key, value, err)
	}
	return tv.GetName(), nil
}

func (c *Client) listBindings(ctx context.Context, parent string) ([]*resourcemanagerpb.TagBinding, error) {
	var bindings []*resourcemanagerpb.TagBinding
	it := c.tagBindings.ListTagBindings(ctx, &resourcemanagerpb.ListTagBindingsRequest{Parent: parent})
	for {
		binding, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return bindings, nil
		}
		if err != nil {
			return nil, fmt.Errorf("listing tag bindings on %s: %w", parent, err)
		}
		bindings = append(bindings, binding)
	}
}

func (c *Client) deleteBinding(ctx context.Context, name string) error {
	op, err := c.tagBindings.DeleteTagBinding(ctx, &resourcemanagerpb.DeleteTagBindingRequest{Name: name})
	if err != nil {
		return err
	}
	return op.Wait(ctx)
}

// parseNamespacedValue splits a namespaced tag value name
// ("{parent}/{key}/{value}") into its key and value short names. It reports
// false for bindings outside the configured tag parent, which this
// controller does not manage.
func (c *Client) parseNamespacedValue(namespacedName string) (key, value string, ok bool) {
	return parseNamespacedValue(namespacedName, c.tagParent)
}

func parseNamespacedValue(namespacedName, tagParent string) (key, value string, ok bool) {
	parts := strings.Split(namespacedName, "/")
	if len(parts) != 3 || parts[0] != tagParent {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// currentTags maps the parent-scoped bindings to key/value short names.
func currentTags(bindings []*resourcemanagerpb.TagBinding, tagParent string) map[string]string {
	tags := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		if key, value, ok := parseNamespacedValue(binding.TagValueNamespacedName, tagParent); ok {
			tags[key] = value
		}
	}
	return tags
}
