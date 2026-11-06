// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/thunder-id/thunderid/tests/integration/flow/common"
	"github.com/thunder-id/thunderid/tests/integration/testutils"
)

const (
	narrowAdminUserType   = "narrow-admin-flow-person"
	narrowAdminPassword   = "NarrowAdmin@123"
	narrowAdminIdentifier = "https://narrow-admin-flow.example.com"

	errCodeNarrowAuthRequired        = "FES-1017"
	errCodeNarrowStartedByAnother    = "FES-1030"
	errCodeNarrowInsufficientPerms   = "FET-1027"
	errCodeNarrowCredentialSetFailed = "FET-1048"
	errCodeNarrowOUCreationFailed    = "FET-1035"
)

// NarrowAdminFlowTestSuite runs administration and onboarding flows as admins who each hold one fine-grained
// system permission in their own unit, granted on a resource server of that unit.
type NarrowAdminFlowTestSuite struct {
	suite.Suite

	signIn     testutils.OUSignInApps
	ouID       string
	userTypeID string
	rsID       string

	creatorToken     string
	deleterToken     string
	viewerToken      string
	groupViewerToken string

	deletionFlowID string
	promptFlowID   string
	resetFlowID    string
	ouFlowID       string

	userIDs []string
	roleIDs []string
}

func TestNarrowAdminFlowTestSuite(t *testing.T) {
	suite.Run(t, new(NarrowAdminFlowTestSuite))
}

func (ts *NarrowAdminFlowTestSuite) SetupSuite() {
	var err error
	ts.ouID, err = testutils.CreateOrganizationUnit(testutils.OrganizationUnit{
		Handle: "narrow-admin-flow-ou", Name: "Narrow Admin Flow OU",
	})
	ts.Require().NoError(err, "create the OU")
	ts.userTypeID, err = testutils.CreateUserType(testutils.UserType{
		Handle: narrowAdminUserType, DisplayName: "Narrow Admin Flow Person", OUID: ts.ouID,
		Schema: map[string]interface{}{
			"username": map[string]interface{}{"type": "string"},
			"email":    map[string]interface{}{"type": "string"},
			"password": map[string]interface{}{"type": "string", "credential": true},
		},
	})
	ts.Require().NoError(err, "create the user type")

	ts.rsID, err = testutils.CreateSystemScopedResourceServer(ts.ouID, "Narrow Admin Flow RS", narrowAdminIdentifier)
	ts.Require().NoError(err, "create the resource server")
	systemIDs, err := testutils.ListResourceIDs(ts.rsID, "")
	ts.Require().NoError(err)
	ts.Require().Len(systemIDs, 1, "the resource server has only its system resource")
	ts.addResource(systemIDs[0], "user", "view", "create", "update", "delete")
	ts.addResource(systemIDs[0], "group", "view")
	ts.addResource(systemIDs[0], "ou", "view", "create")

	ts.creatorToken = ts.adminToken("creator", "system:user:create")
	ts.deleterToken = ts.adminToken("deleter", "system:user:delete")
	ts.viewerToken = ts.adminToken("viewer", "system:user:view")
	ts.groupViewerToken = ts.adminToken("group-viewer", "system:group:view")

	ts.deletionFlowID, err = testutils.GetFlowIDByHandle(deletionFlowHandle, administrationFlowType)
	ts.Require().NoError(err, "resolve the shipped user deletion flow")

	ts.promptFlowID = ts.createFlow("narrow-admin-prompt-flow", []map[string]interface{}{
		{"id": "start", "type": "START", "onSuccess": "prompt_note"},
		{
			"id": "prompt_note", "type": "PROMPT",
			"prompts": []map[string]interface{}{{
				"inputs": []map[string]interface{}{
					{"ref": "note_input", "identifier": "note", "type": "TEXT_INPUT", "required": true},
				},
				"action": map[string]interface{}{"ref": "action_note", "nextNode": "end"},
			}},
		},
		{"id": "end", "type": "END"},
	})
	ts.resetFlowID = ts.createFlow("narrow-admin-reset-flow", []map[string]interface{}{
		{"id": "start", "type": "START", "onSuccess": "permission_validator"},
		permissionValidatorNode("system:user:view", "identify"),
		{
			"id": "identify", "type": "TASK_EXECUTION",
			"executor":  map[string]interface{}{"name": "IdentifyingExecutor", "mode": "identify"},
			"onSuccess": "set_password",
		},
		{
			"id": "set_password", "type": "TASK_EXECUTION",
			"executor":  map[string]interface{}{"name": "CredentialSetter"},
			"onSuccess": "end",
		},
		{"id": "end", "type": "END"},
	})
	ts.ouFlowID = ts.createFlow("narrow-admin-ou-flow", []map[string]interface{}{
		{"id": "start", "type": "START", "onSuccess": "permission_validator"},
		permissionValidatorNode("system:group:view", "create_ou"),
		{
			"id": "create_ou", "type": "TASK_EXECUTION",
			"executor":   map[string]interface{}{"name": "OUExecutor"},
			"properties": map[string]interface{}{"parentOuId": ts.ouID},
			"onSuccess":  "end",
		},
		{"id": "end", "type": "END"},
	})
}

func (ts *NarrowAdminFlowTestSuite) TearDownSuite() {
	if err := ts.signIn.DeleteAll(); err != nil {
		ts.T().Logf("teardown: delete sign-in applications: %v", err)
	}
	for _, id := range []string{ts.promptFlowID, ts.resetFlowID, ts.ouFlowID} {
		if id != "" {
			if err := testutils.DeleteFlow(id); err != nil {
				ts.T().Logf("teardown: delete flow %s: %v", id, err)
			}
		}
	}
	for _, id := range ts.roleIDs {
		if err := testutils.DeleteRole(id); err != nil {
			ts.T().Logf("teardown: delete role %s: %v", id, err)
		}
	}
	for _, id := range ts.userIDs {
		if err := testutils.DeleteUser(id); err != nil {
			ts.T().Logf("teardown: delete user %s: %v", id, err)
		}
	}
	if ts.rsID != "" {
		if err := testutils.DeleteResourceServerWithChildren(ts.rsID); err != nil {
			ts.T().Logf("teardown: delete resource server: %v", err)
		}
	}
	if ts.userTypeID != "" {
		if err := testutils.DeleteUserType(ts.userTypeID); err != nil {
			ts.T().Logf("teardown: delete user type: %v", err)
		}
	}
	if ts.ouID != "" {
		if err := testutils.DeleteOrganizationUnit(ts.ouID); err != nil {
			ts.T().Logf("teardown: delete OU: %v", err)
		}
	}
}

// An admin holding only the user create permission runs the shipped onboarding flow to a new user.
func (ts *NarrowAdminFlowTestSuite) TestCreator_CompletesTheDefaultUserOnboardingFlow() {
	username := common.GenerateUniqueUsername("narrow_onboarded")

	step := ts.run(ts.creatorToken, map[string]interface{}{
		"flowType": "USER_ONBOARDING",
		"inputs":   map[string]string{"userType": narrowAdminUserType, "ouId": ts.ouID},
	})
	ts.Require().Equal("INCOMPLETE", step.FlowStatus, "%+v", step)
	ts.Require().Nil(step.Error, "%+v", step.Error)
	step = ts.run(ts.creatorToken, map[string]interface{}{
		"executionId": step.ExecutionID, "challengeToken": step.ChallengeToken, "action": "action_create_user_now",
	})
	ts.Require().Nil(step.Error, "%+v", step.Error)
	ts.Require().True(common.HasInput(step.Data.Inputs, "username"), "%+v", step)
	step = ts.run(ts.creatorToken, map[string]interface{}{
		"executionId": step.ExecutionID, "challengeToken": step.ChallengeToken, "action": "action_create_user_details",
		"inputs": map[string]string{
			"username": username, "email": username + "@narrow.test", "password": narrowAdminPassword,
		},
	})

	ts.Require().Nil(step.Error, "%+v", step.Error)
	user, err := testutils.FindUserByAttribute("username", username)
	ts.Require().NoError(err)
	ts.Require().NotNil(user, "the flow provisions the user")
	ts.userIDs = append(ts.userIDs, user.ID)
	ts.Equal(ts.ouID, user.OUID)
}

func (ts *NarrowAdminFlowTestSuite) TestUserOnboardingWithoutAToken_Refused() {
	result, err := common.ExecuteFlowWithToken("", map[string]interface{}{"flowType": "USER_ONBOARDING"})

	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, result.StatusCode)
	ts.Require().NotNil(result.Error)
	ts.Equal(errCodeNarrowAuthRequired, result.Error.Code)
}

// An admin holding only the user delete permission runs the shipped deletion flow.
func (ts *NarrowAdminFlowTestSuite) TestDeleter_RunsTheDefaultUserDeletionFlow() {
	userID := ts.createUser(common.GenerateUniqueUsername("narrow_deleted"))

	step := ts.run(ts.deleterToken, map[string]interface{}{
		"flowId": ts.deletionFlowID, "inputs": map[string]string{deletionSubjectInput: userID},
	})

	ts.Equal("COMPLETE", step.FlowStatus, "%+v", step.Error)
	ts.False(ts.userExists(userID), "the flow deletes the user")
	ts.userIDs = slices.DeleteFunc(ts.userIDs, func(id string) bool { return id == userID })
}

// The deletion flow's Permission Validator names the delete permission, which a viewer does not hold.
func (ts *NarrowAdminFlowTestSuite) TestViewer_IsRefusedTheDefaultUserDeletionFlow() {
	userID := ts.createUser(common.GenerateUniqueUsername("narrow_kept"))

	step := ts.run(ts.viewerToken, map[string]interface{}{
		"flowId": ts.deletionFlowID, "inputs": map[string]string{deletionSubjectInput: userID},
	})

	ts.NotEqual("COMPLETE", step.FlowStatus)
	ts.Require().NotNil(step.Error, "%+v", step)
	ts.Equal(errCodeNarrowInsufficientPerms, step.Error.Code)
	ts.True(ts.userExists(userID), "the user is kept")
}

// An administration flow is its starter's: another admin cannot send its next step, and neither can a caller
// without a token.
func (ts *NarrowAdminFlowTestSuite) TestAnotherAdmin_CannotContinueTheStartersFlow() {
	step := ts.run(ts.groupViewerToken, map[string]interface{}{"flowId": ts.promptFlowID})
	ts.Require().Equal("INCOMPLETE", step.FlowStatus, "%+v", step)
	next := map[string]interface{}{
		"executionId": step.ExecutionID, "challengeToken": step.ChallengeToken, "action": "action_note",
		"inputs": map[string]string{"note": "hello"},
	}

	other, err := common.ExecuteFlowWithToken(ts.creatorToken, next)
	ts.Require().NoError(err)
	ts.Equal(http.StatusForbidden, other.StatusCode)
	ts.Require().NotNil(other.Error)
	ts.Equal(errCodeNarrowStartedByAnother, other.Error.Code)

	anonymous, err := common.ExecuteFlowWithToken("", next)
	ts.Require().NoError(err)
	ts.Equal(http.StatusUnauthorized, anonymous.StatusCode)
	ts.Require().NotNil(anonymous.Error)
	ts.Equal(errCodeNarrowAuthRequired, anonymous.Error.Code)
}

// A refused step ends the execution, so the starter's own flow is checked on an execution of its own.
func (ts *NarrowAdminFlowTestSuite) TestStarter_ContinuesTheirOwnFlow() {
	step := ts.run(ts.groupViewerToken, map[string]interface{}{"flowId": ts.promptFlowID})
	ts.Require().Equal("INCOMPLETE", step.FlowStatus, "%+v", step)

	step = ts.run(ts.groupViewerToken, map[string]interface{}{
		"executionId": step.ExecutionID, "challengeToken": step.ChallengeToken, "action": "action_note",
		"inputs": map[string]string{"note": "hello"},
	})

	ts.Equal("COMPLETE", step.FlowStatus, "%+v", step.Error)
}

// A flow an admin authors runs with that admin's authority, so a viewer cannot set another user's password.
func (ts *NarrowAdminFlowTestSuite) TestViewersOwnFlow_CannotResetAnotherUsersPassword() {
	username := common.GenerateUniqueUsername("narrow_reset")
	ts.createUser(username)

	step := ts.run(ts.viewerToken, map[string]interface{}{
		"flowId": ts.resetFlowID,
		"inputs": map[string]string{"username": username, "password": "Changed@123"},
	})

	ts.NotEqual("COMPLETE", step.FlowStatus)
	ts.Require().NotNil(step.Error, "%+v", step)
	ts.Equal(errCodeNarrowCredentialSetFailed, step.Error.Code)
}

// The organization unit service checks the create permission on the parent, which the admin does not hold.
func (ts *NarrowAdminFlowTestSuite) TestGroupViewersOwnFlow_CannotCreateAnOU() {
	const childPath = "narrow-admin-flow-ou/narrow-admin-child"
	ts.T().Cleanup(func() {
		if ts.status(http.MethodGet, "/organization-units/tree/"+childPath) == http.StatusOK {
			ts.NoError(testutils.DeleteOrganizationUnitByHandlePath(childPath))
		}
	})
	step := ts.run(ts.groupViewerToken, map[string]interface{}{
		"flowId": ts.ouFlowID,
		"inputs": map[string]string{"ouName": "Narrow Admin Child", "ouHandle": "narrow-admin-child"},
	})

	ts.NotEqual("COMPLETE", step.FlowStatus)
	ts.Require().NotNil(step.Error, "%+v", step)
	ts.Equal(errCodeNarrowOUCreationFailed, step.Error.Code)
	ts.Equal(http.StatusNotFound,
		ts.status(http.MethodGet, "/organization-units/tree/"+childPath))
}

// addResource adds a resource under parentID with the actions named.
func (ts *NarrowAdminFlowTestSuite) addResource(parentID, handle string, actions ...string) {
	resourceID, err := testutils.CreateResource(ts.rsID, handle, handle, parentID)
	ts.Require().NoError(err, "create the %s resource", handle)
	for _, action := range actions {
		_, err := testutils.CreateResourceAction(ts.rsID, resourceID, testutils.Action{Name: action, Handle: action})
		ts.Require().NoError(err, "create the %s action on %s", action, handle)
	}
}

// adminToken makes a user of the suite's unit hold permission through a role, and returns their token.
func (ts *NarrowAdminFlowTestSuite) adminToken(name, permission string) string {
	username := "narrow-admin-" + name
	userID := ts.createUser(username)
	createRole(&ts.Suite, &ts.roleIDs, ts.ouID, ts.rsID, "Narrow Admin "+name, []string{permission},
		testutils.Assignment{ID: userID, Type: "user"})
	token, err := ts.signIn.ObtainAccessToken(ts.ouID, permission, username, narrowAdminPassword,
		narrowAdminIdentifier)
	ts.Require().NoError(err, "obtain the %s token", name)
	return token.AccessToken
}

func (ts *NarrowAdminFlowTestSuite) createUser(username string) string {
	id, err := testutils.CreateUser(testutils.User{
		Type: narrowAdminUserType, OUID: ts.ouID,
		Attributes: json.RawMessage(fmt.Sprintf(`{"username": %q, "email": %q, "password": %q}`,
			username, username+"@narrow.test", narrowAdminPassword)),
	})
	ts.Require().NoError(err, "create user %s", username)
	ts.userIDs = append(ts.userIDs, id)
	return id
}

func (ts *NarrowAdminFlowTestSuite) createFlow(handle string, nodes []map[string]interface{}) string {
	id, err := testutils.CreateFlow(testutils.Flow{
		Name: handle, FlowType: administrationFlowType, Handle: handle, Nodes: nodes, OUID: ts.ouID,
	})
	ts.Require().NoError(err, "create flow %s", handle)
	return id
}

// permissionValidatorNode requires permission, then moves on to next.
func permissionValidatorNode(permission, next string) map[string]interface{} {
	return map[string]interface{}{
		"id": "permission_validator", "type": "TASK_EXECUTION",
		"executor":   map[string]interface{}{"name": "PermissionValidator"},
		"properties": map[string]interface{}{"requiredScopes": []string{permission}},
		"onSuccess":  next,
	}
}

// run sends a flow request as the token's caller and requires a step back.
func (ts *NarrowAdminFlowTestSuite) run(token string, request map[string]interface{}) *common.FlowStep {
	result, err := common.ExecuteFlowWithToken(token, request)
	ts.Require().NoError(err)
	ts.Require().Equal(http.StatusOK, result.StatusCode, "%s", string(result.Body))
	return result.Step
}

func (ts *NarrowAdminFlowTestSuite) userExists(userID string) bool {
	return ts.status(http.MethodGet, "/users/"+userID) == http.StatusOK
}

// status sends a request as the suite's root admin and returns the response status.
func (ts *NarrowAdminFlowTestSuite) status(method, path string) int {
	req, err := http.NewRequest(method, testutils.TestServerURL+path, nil)
	ts.Require().NoError(err)
	resp, err := testutils.GetHTTPClient().Do(req)
	ts.Require().NoError(err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
