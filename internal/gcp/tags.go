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
)

// ErrTagNotFound indicates a tag key or value short name does not exist under
// the configured organization. The controller only binds pre-existing tags;
// it surfaces this in status and keeps requeuing, since tags can be created
// in GCP out-of-band.
var ErrTagNotFound = errors.New("tag key or value not found in GCP organization")

// TagBinder reconciles Resource Manager tag bindings on GSM secrets.
type TagBinder interface {
	// SyncTags performs a full sync of tag bindings on the named secret:
	// bindings are added for entries in desired, and bindings previously
	// managed by the controller but absent from desired are removed.
	// It returns the tags bound after the sync.
	SyncTags(ctx context.Context, secretName string, desired map[string]string) (map[string]string, error)

	// RemoveTags removes all controller-managed tag bindings from the named
	// secret. Used when a GSMSecretTag with deletionPolicy Delete is deleted.
	RemoveTags(ctx context.Context, secretName string) error
}

// Client implements TagBinder against the GCP Resource Manager API using
// Workload Identity credentials from the environment.
type Client struct {
	// OrganizationID is the numeric GCP organization ID under which tag key
	// and value short names are resolved (organizations/{id}).
	OrganizationID string
	// ProjectID is the GCP project holding the GSM secrets, used to build
	// the full resource name for tag bindings:
	// //secretmanager.googleapis.com/projects/{project}/secrets/{name}.
	ProjectID string
}

// NewClient returns a TagBinder scoped to the given organization and project.
func NewClient(organizationID, projectID string) *Client {
	return &Client{OrganizationID: organizationID, ProjectID: projectID}
}

// secretResourceName builds the full resource name required by the tag
// bindings API.
func (c *Client) secretResourceName(secretName string) string {
	return fmt.Sprintf("//secretmanager.googleapis.com/projects/%s/secrets/%s", c.ProjectID, secretName)
}

func (c *Client) SyncTags(ctx context.Context, secretName string, desired map[string]string) (map[string]string, error) {
	// TODO: implement using cloud.google.com/go/resourcemanager/apiv3:
	//   1. Resolve each short name in desired to tagKeys/{id}/tagValues/{id}
	//      under organizations/{c.OrganizationID}; return ErrTagNotFound if
	//      a key or value does not exist.
	//   2. List existing tag bindings on c.secretResourceName(secretName).
	//   3. Create bindings missing from GCP, delete bindings absent from
	//      desired (full sync).
	_ = c.secretResourceName(secretName)
	return nil, errors.New("not implemented")
}

func (c *Client) RemoveTags(ctx context.Context, secretName string) error {
	// TODO: list and delete all tag bindings on
	// c.secretResourceName(secretName).
	return errors.New("not implemented")
}
