// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/thunder-id/thunderid/tests/integration/testutils"
)

// ApplicationAuthzTestSuite validates that application operations respect the application
// permissions and OU-scoped authz.
//
// Permission model:
//
//	system:app:view → list and get applications
//	system:app      → every application action
//
// An app-manager living in OU1 holds system:app. The suite verifies that:
//
//   - Reads and writes on applications in OU1 are allowed
//   - Reads and writes on applications in OU2 (sibling) are denied (403)
//   - Moving an application from OU1 into OU2 is denied
//   - Listing applications only returns applications from the accessible OU
//   - A token holding only system:app:view can read but not write
//
// Fixture topology:
//
//	OU1 (handle: authz-app-ou1) ← app-manager and target applications belong here
//	OU2 (handle: authz-app-ou2) ← sibling OU with its own target application
type ApplicationAuthzTestSuite struct {
	suite.Suite

	appOU1ID string
	appOU2ID string

	userTypeOU1ID string
	appMgrRoleID  string
	appMgrUserID  string
	scopedRSID    string

	targetAppOU1ID    string
	deletableAppOU1ID string
	targetAppOU2ID    string

	// HTTP clients carrying the app-manager's system:app and system:app:view scoped tokens
	appAdminClient  *http.Client
	appViewerClient *http.Client
}

const (
	appAuthzOU1Handle = "authz-app-ou1"
	appAuthzOU2Handle = "authz-app-ou2"

	appMgrUsername         = "authz-app-manager"
	appMgrPassword         = "AppMgr@123"
	appMgrRoleName         = "Application Admin (app-authz-test)"
	appMgrUserType         = "authz-app-mgr-type"
	appScopedRSIdentifier  = "https://authz-test.example.com/app"
	appAuthzConsoleClient  = "CONSOLE"
	appAuthzConsoleURLPath = "/console"
)

func TestApplicationAuthzTestSuite(t *testing.T) {
	suite.Run(t, new(ApplicationAuthzTestSuite))
}

func (ts *ApplicationAuthzTestSuite) SetupSuite() {
	ou1ID, err := testutils.CreateOrganizationUnit(testutils.OrganizationUnit{
		Handle:      appAuthzOU1Handle,
		Name:        "App Authz Test OU1",
		Description: "Primary OU for application authz integration test",
	})
	ts.Require().NoError(err, "create app-authz OU1")
	ts.appOU1ID = ou1ID

	ou2ID, err := testutils.CreateOrganizationUnit(testutils.OrganizationUnit{
		Handle:      appAuthzOU2Handle,
		Name:        "App Authz Test OU2",
		Description: "Sibling OU for application authz integration test",
	})
	ts.Require().NoError(err, "create app-authz OU2")
	ts.appOU2ID = ou2ID

	userTypeID, err := testutils.CreateUserType(testutils.UserType{
		Handle:      appMgrUserType,
		DisplayName: "Authz App Mgr Type",
		OUID:        ts.appOU1ID,
		Schema: map[string]interface{}{
			"username":     map[string]interface{}{"type": "string"},
			"password":     map[string]interface{}{"type": "string", "credential": true},
			"display_name": map[string]interface{}{"type": "string"},
		},
	})
	ts.Require().NoError(err, "create user type for app-manager")
	ts.userTypeOU1ID = userTypeID

	appMgrID, err := testutils.CreateUser(testutils.User{
		Type: appMgrUserType,
		OUID: ts.appOU1ID,
		Attributes: json.RawMessage(fmt.Sprintf(
			`{"username": %q, "password": %q, "display_name": "App Manager"}`,
			appMgrUsername, appMgrPassword,
		)),
	})
	ts.Require().NoError(err, "create app-manager user")
	ts.appMgrUserID = appMgrID

	ts.targetAppOU1ID, err = testutils.CreateApplication(testutils.Application{
		OUID: ts.appOU1ID, Name: "authz-target-app-ou1"})
	ts.Require().NoError(err, "create target application in OU1")
	ts.deletableAppOU1ID, err = testutils.CreateApplication(testutils.Application{
		OUID: ts.appOU1ID, Name: "authz-deletable-app-ou1"})
	ts.Require().NoError(err, "create deletable application in OU1")
	ts.targetAppOU2ID, err = testutils.CreateApplication(testutils.Application{
		OUID: ts.appOU2ID, Name: "authz-target-app-ou2"})
	ts.Require().NoError(err, "create target application in OU2")

	// A custom resource server reproduces "system:app" and "system:app:view", so the suite does not
	// depend on how the System resource server issues them.
	rsID, err := testutils.CreateSystemScopedResourceServer(
		ts.appOU1ID, "Authz Test RS (app)", appScopedRSIdentifier, "app")
	ts.Require().NoError(err, "create scoped resource server")
	ts.scopedRSID = rsID

	roleID, err := testutils.CreateRole(testutils.Role{
		Name: appMgrRoleName,
		OUID: ts.appOU1ID,
		Permissions: []testutils.ResourcePermissions{
			{ResourceServerID: rsID, Permissions: []string{"system:app", "system:app:view"}},
		},
		Assignments: []testutils.Assignment{{ID: ts.appMgrUserID, Type: "user"}},
	})
	ts.Require().NoError(err, "create app-manager role")
	ts.appMgrRoleID = roleID

	ts.appAdminClient = ts.obtainClient("system:app")
	ts.appViewerClient = ts.obtainClient("system:app:view")
}

// obtainClient returns an HTTP client carrying an app-manager token issued for the given scope.
func (ts *ApplicationAuthzTestSuite) obtainClient(scope string) *http.Client {
	tokenResp, err := testutils.ObtainAccessTokenWithPassword(
		appAuthzConsoleClient,
		testutils.TestServerURL+appAuthzConsoleURLPath,
		scope,
		appMgrUsername,
		appMgrPassword,
		true,
		"",
		appScopedRSIdentifier,
	)
	ts.Require().NoError(err, "obtain app-manager token for %q", scope)
	ts.Require().NotEmpty(tokenResp.AccessToken)
	return testutils.GetHTTPClientWithToken(tokenResp.AccessToken)
}

func (ts *ApplicationAuthzTestSuite) TearDownSuite() {
	if ts.appMgrRoleID != "" {
		if err := testutils.DeleteRole(ts.appMgrRoleID); err != nil {
			ts.T().Logf("teardown: delete app-manager role: %v", err)
		}
	}
	if ts.scopedRSID != "" {
		if err := testutils.DeleteResourceServerWithChildren(ts.scopedRSID); err != nil {
			ts.T().Errorf("teardown: delete scoped resource server: %v", err)
		}
	}
	for _, id := range []string{ts.targetAppOU1ID, ts.deletableAppOU1ID, ts.targetAppOU2ID} {
		if id != "" {
			if err := testutils.DeleteApplication(id); err != nil {
				ts.T().Logf("teardown: delete application %s: %v", id, err)
			}
		}
	}
	if ts.appMgrUserID != "" {
		if err := testutils.DeleteUser(ts.appMgrUserID); err != nil {
			ts.T().Logf("teardown: delete app-manager user: %v", err)
		}
	}
	if ts.userTypeOU1ID != "" {
		if err := testutils.DeleteUserType(ts.userTypeOU1ID); err != nil {
			ts.T().Logf("teardown: delete app-manager user type: %v", err)
		}
	}
	for _, id := range []string{ts.appOU2ID, ts.appOU1ID} {
		if id != "" {
			if err := testutils.DeleteOrganizationUnit(id); err != nil {
				ts.T().Logf("teardown: delete OU %s: %v", id, err)
			}
		}
	}
}

func (ts *ApplicationAuthzTestSuite) do(client *http.Client, method, path string, body interface{}) *http.Response {
	ts.T().Helper()

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		ts.Require().NoError(err)
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, testutils.TestServerURL+path, bodyReader)
	ts.Require().NoError(err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	ts.Require().NoError(err)
	return resp
}

// appPayload is the smallest application a create or full-replacement update accepts.
func appPayload(name, ouID string) map[string]interface{} {
	return map[string]interface{}{
		"name": name,
		"ouId": ouID,
		"type": "fullstack",
		"inboundAuthConfig": []map[string]interface{}{{
			"type": "oauth2",
			"config": map[string]interface{}{
				"redirectUris": []string{"http://localhost:8080/callback"},
				"grantTypes":   []string{"client_credentials"},
			},
		}},
	}
}

// appOUID returns the application's organization unit, read with the root admin client.
func (ts *ApplicationAuthzTestSuite) appOUID(appID string) (string, int) {
	ts.T().Helper()
	resp := ts.do(testutils.GetHTTPClient(), http.MethodGet, "/applications/"+appID, nil)
	defer resp.Body.Close()
	var app struct {
		OUID string `json:"ouId"`
	}
	if resp.StatusCode == http.StatusOK {
		ts.Require().NoError(json.NewDecoder(resp.Body).Decode(&app))
	}
	return app.OUID, resp.StatusCode
}

func (ts *ApplicationAuthzTestSuite) TestListApplications() {
	resp := ts.do(ts.appViewerClient, http.MethodGet, "/applications", nil)
	defer resp.Body.Close()
	ts.Require().Equal(http.StatusOK, resp.StatusCode)

	var list struct {
		Applications []struct {
			ID string `json:"id"`
		} `json:"applications"`
	}
	ts.Require().NoError(json.NewDecoder(resp.Body).Decode(&list))
	ids := make([]string, 0, len(list.Applications))
	for _, app := range list.Applications {
		ids = append(ids, app.ID)
	}
	ts.Contains(ids, ts.targetAppOU1ID, "list must include the application in OU1")
	ts.NotContains(ids, ts.targetAppOU2ID, "list must not include the application in OU2")
}

func (ts *ApplicationAuthzTestSuite) TestGetApplication() {
	resp := ts.do(ts.appViewerClient, http.MethodGet, "/applications/"+ts.targetAppOU1ID, nil)
	resp.Body.Close()
	ts.Equal(http.StatusOK, resp.StatusCode, "a viewer reads an application in their own OU")

	resp = ts.do(ts.appAdminClient, http.MethodGet, "/applications/"+ts.targetAppOU2ID, nil)
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode, "an application in another OU must not be readable")
}

func (ts *ApplicationAuthzTestSuite) TestViewerCannotWrite() {
	resp := ts.do(ts.appViewerClient, http.MethodPost, "/applications",
		appPayload("authz-viewer-created-app", ts.appOU1ID))
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode, "system:app:view must not create")

	resp = ts.do(ts.appViewerClient, http.MethodPut, "/applications/"+ts.targetAppOU1ID,
		appPayload("authz-target-app-ou1", ts.appOU1ID))
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode, "system:app:view must not update")

	resp = ts.do(ts.appViewerClient, http.MethodDelete, "/applications/"+ts.targetAppOU1ID, nil)
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode, "system:app:view must not delete")
}

func (ts *ApplicationAuthzTestSuite) TestCreateApplication() {
	resp := ts.do(ts.appAdminClient, http.MethodPost, "/applications",
		appPayload("authz-created-app", ts.appOU1ID))
	defer resp.Body.Close()
	ts.Equal(http.StatusCreated, resp.StatusCode, "the app-manager creates an application in their own OU")
	var created struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(resp.Body).Decode(&created) == nil && created.ID != "" {
		if err := testutils.DeleteApplication(created.ID); err != nil {
			ts.T().Logf("cleanup: delete created application %s: %v", created.ID, err)
		}
	}

	denied := ts.do(ts.appAdminClient, http.MethodPost, "/applications",
		appPayload("authz-denied-app", ts.appOU2ID))
	denied.Body.Close()
	ts.Equal(http.StatusForbidden, denied.StatusCode, "the app-manager must not create in another OU")
}

func (ts *ApplicationAuthzTestSuite) TestUpdateApplication() {
	resp := ts.do(ts.appAdminClient, http.MethodPut, "/applications/"+ts.targetAppOU1ID,
		appPayload("authz-target-app-ou1", ts.appOU1ID))
	resp.Body.Close()
	ts.Equal(http.StatusOK, resp.StatusCode, "the app-manager updates an application in their own OU")

	resp = ts.do(ts.appAdminClient, http.MethodPut, "/applications/"+ts.targetAppOU2ID,
		appPayload("authz-target-app-ou2", ts.appOU2ID))
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode, "the app-manager must not update in another OU")
}

func (ts *ApplicationAuthzTestSuite) TestMoveApplicationToOtherOU() {
	resp := ts.do(ts.appAdminClient, http.MethodPut, "/applications/"+ts.targetAppOU1ID,
		appPayload("authz-target-app-ou1", ts.appOU2ID))
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode,
		"the app-manager must not move an application into an OU they do not administer")

	ouID, status := ts.appOUID(ts.targetAppOU1ID)
	ts.Require().Equal(http.StatusOK, status)
	ts.Equal(ts.appOU1ID, ouID, "a denied move must not change the application's OU")
}

func (ts *ApplicationAuthzTestSuite) TestDeleteApplication() {
	resp := ts.do(ts.appAdminClient, http.MethodDelete, "/applications/"+ts.targetAppOU2ID, nil)
	resp.Body.Close()
	ts.Equal(http.StatusForbidden, resp.StatusCode, "the app-manager must not delete in another OU")
	_, status := ts.appOUID(ts.targetAppOU2ID)
	ts.Equal(http.StatusOK, status, "a denied delete must leave the application in place")

	resp = ts.do(ts.appAdminClient, http.MethodDelete, "/applications/"+ts.deletableAppOU1ID, nil)
	resp.Body.Close()
	ts.Equal(http.StatusNoContent, resp.StatusCode, "the app-manager deletes an application in their own OU")
	_, status = ts.appOUID(ts.deletableAppOU1ID)
	ts.Equal(http.StatusNotFound, status, "the deleted application must be gone")
	ts.deletableAppOU1ID = ""
}
