// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package application

import (
	"context"
	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/thunder-id/thunderid/internal/application/model"
	"github.com/thunder-id/thunderid/internal/entity"
	inboundmodel "github.com/thunder-id/thunderid/internal/inboundclient/model"
	"github.com/thunder-id/thunderid/internal/sharing"
	"github.com/thunder-id/thunderid/internal/system/config"
	"github.com/thunder-id/thunderid/internal/system/security"
	"github.com/thunder-id/thunderid/internal/system/sysauthz"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/inboundclientmock"
	"github.com/thunder-id/thunderid/tests/mocks/sharingmock"
)

const (
	authzOwnOU   = "ou-own"
	authzOtherOU = "ou-other"
)

// setupAuthzTestService returns the test service with the real system authorization service, so the
// organization unit boundary is evaluated rather than mocked.
func (suite *ServiceTestSuite) setupAuthzTestService() (
	*applicationService, *inboundclientmock.InboundClientServiceInterfaceMock) {
	security.InitSystemPermissions("")
	service, mockStore := suite.setupTestService()
	authz, err := sysauthz.Initialize()
	suite.Require().NoError(err)
	service.authzService = authz
	return service, mockStore
}

// callerIn returns a context for a caller in the given organization unit holding the permissions.
func callerIn(ouID string, permissions ...string) context.Context {
	return security.WithSecurityContextTest(context.Background(),
		security.NewSecurityContextForTest("admin-1", ouID, "", permissions, nil))
}

func (suite *ServiceTestSuite) assertUnauthorized(svcErr *tidcommon.ServiceError) {
	suite.Require().NotNil(svcErr)
	assert.Equal(suite.T(), tidcommon.ErrorUnauthorized.Code, svcErr.Code)
}

func (suite *ServiceTestSuite) TestAuthz_GetApplication() {
	cases := []struct {
		name        string
		appOU       string
		permissions []string
		allowed     bool
	}{
		{"ViewInOwnOU", authzOwnOU, []string{"system:app:view"}, true},
		{"ResourcePermissionInOwnOU", authzOwnOU, []string{"system:app"}, true},
		{"ViewInAnotherOU", authzOtherOU, []string{"system:app:view"}, false},
		{"CreateOnlyCannotRead", authzOwnOU, []string{"system:app:create"}, false},
		{"UpdateOnlyCannotRead", authzOwnOU, []string{"system:app:update"}, false},
		{"OtherResourceCannotRead", authzOwnOU, []string{"system:user"}, false},
		{"RootInAnotherOU", authzOtherOU, []string{"system"}, true},
	}
	for _, tc := range cases {
		suite.Run(tc.name, func() {
			service, mockStore := suite.setupAuthzTestService()
			mockLoadFullApplication(mockStore, service,
				&model.ApplicationProcessedDTO{ID: testServiceAppID, Name: "App", OUID: tc.appOU})

			result, svcErr := service.GetApplication(callerIn(authzOwnOU, tc.permissions...), testServiceAppID)

			if tc.allowed {
				suite.Require().Nil(svcErr)
				assert.Equal(suite.T(), testServiceAppID, result.ID)
			} else {
				assert.Nil(suite.T(), result)
				suite.assertUnauthorized(svcErr)
			}
		})
	}
}

func (suite *ServiceTestSuite) TestAuthz_GetApplication_RuntimeContextIsAllowed() {
	service, mockStore := suite.setupAuthzTestService()
	mockLoadFullApplication(mockStore, service,
		&model.ApplicationProcessedDTO{ID: testServiceAppID, Name: "App", OUID: authzOtherOU})

	result, svcErr := service.GetApplication(security.WithRuntimeContext(context.Background()), testServiceAppID)

	suite.Require().Nil(svcErr)
	assert.Equal(suite.T(), testServiceAppID, result.ID)
}

func (suite *ServiceTestSuite) TestAuthz_GetApplication_UnknownOUIsRootOnly() {
	for _, tc := range []struct {
		name        string
		permissions []string
		allowed     bool
	}{
		{"FineGrainedRefused", []string{"system:app"}, false},
		{"RootAllowed", []string{"system"}, true},
	} {
		suite.Run(tc.name, func() {
			service, mockStore := suite.setupAuthzTestService()
			mockLoadFullApplication(mockStore, service,
				&model.ApplicationProcessedDTO{ID: testServiceAppID, Name: "App"})

			_, svcErr := service.GetApplication(callerIn(authzOwnOU, tc.permissions...), testServiceAppID)

			if tc.allowed {
				assert.Nil(suite.T(), svcErr)
			} else {
				suite.assertUnauthorized(svcErr)
			}
		})
	}
}

func (suite *ServiceTestSuite) TestAuthz_CreateApplication() {
	cases := []struct {
		name        string
		targetOU    string
		permissions []string
		allowed     bool
	}{
		{"CreateInOwnOU", authzOwnOU, []string{"system:app:create"}, true},
		{"CreateInAnotherOU", authzOtherOU, []string{"system:app:create"}, false},
		{"UpdateCannotCreate", authzOwnOU, []string{"system:app:update"}, false},
		{"ViewCannotCreate", authzOwnOU, []string{"system:app:view"}, false},
		{"DeleteCannotCreate", authzOwnOU, []string{"system:app:delete"}, false},
		{"RootInAnotherOU", authzOtherOU, []string{"system"}, true},
	}
	for _, tc := range cases {
		suite.Run(tc.name, func() {
			config.ResetServerRuntime()
			suite.Require().NoError(config.InitializeServerRuntime("/tmp/test", &config.Config{}))
			defer config.ResetServerRuntime()
			service, mockStore := suite.setupAuthzTestService()
			// A create that passes authorization reaches the store, which fails it here.
			mockStore.On("CreateInboundClient", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Maybe().Return(errors.New("store error"))

			_, svcErr := service.CreateApplication(callerIn(authzOwnOU, tc.permissions...), &model.ApplicationDTO{
				Type: model.ApplicationTypeFullStack,
				Name: "Test App",
				OUID: tc.targetOU,
				InboundAuthProfile: providers.InboundAuthProfile{
					AuthFlowID:         "edc013d0-e893-4dc0-990c-3e1d203e005b",
					RegistrationFlowID: "80024fb3-29ed-4c33-aa48-8aee5e96d522",
				},
			})

			if tc.allowed {
				assert.Equal(suite.T(), &tidcommon.InternalServerError, svcErr)
			} else {
				suite.assertUnauthorized(svcErr)
				mockStore.AssertNotCalled(suite.T(), "CreateInboundClient",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}

func (suite *ServiceTestSuite) TestAuthz_UpdateApplication() {
	cases := []struct {
		name        string
		appOU       string
		targetOU    string
		permissions []string
		allowed     bool
	}{
		{"UpdateInOwnOU", authzOwnOU, authzOwnOU, []string{"system:app:update"}, true},
		{"UpdateInAnotherOU", authzOtherOU, authzOtherOU, []string{"system:app:update"}, false},
		{"MoveIntoAnotherOU", authzOwnOU, authzOtherOU, []string{"system:app:update"}, false},
		{"MoveOutOfAnotherOU", authzOtherOU, authzOwnOU, []string{"system:app:update"}, false},
		{"CreateCannotUpdate", authzOwnOU, authzOwnOU, []string{"system:app:create"}, false},
		{"ViewCannotUpdate", authzOwnOU, authzOwnOU, []string{"system:app:view"}, false},
		{"RootInAnotherOU", authzOtherOU, authzOtherOU, []string{"system"}, true},
	}
	for _, tc := range cases {
		suite.Run(tc.name, func() {
			config.ResetServerRuntime()
			suite.Require().NoError(config.InitializeServerRuntime("/tmp/test", &config.Config{}))
			defer config.ResetServerRuntime()
			service, mockStore := suite.setupAuthzTestService()
			mockLoadFullApplication(mockStore, service,
				&model.ApplicationProcessedDTO{ID: testServiceAppID, Name: "App", OUID: tc.appOU})
			// An update that passes authorization reaches the store, which fails it here.
			mockStore.On("UpdateInboundClient",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Maybe().Return(errors.New("store error"))

			_, svcErr := service.UpdateApplication(callerIn(authzOwnOU, tc.permissions...), testServiceAppID,
				&model.ApplicationDTO{Name: "App", OUID: tc.targetOU})

			if tc.allowed {
				assert.Equal(suite.T(), &tidcommon.InternalServerError, svcErr)
			} else {
				suite.assertUnauthorized(svcErr)
				mockStore.AssertNotCalled(suite.T(), "UpdateInboundClient",
					mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}

func (suite *ServiceTestSuite) TestAuthz_DeleteApplication() {
	cases := []struct {
		name        string
		appOU       string
		permissions []string
		allowed     bool
	}{
		{"DeleteInOwnOU", authzOwnOU, []string{"system:app:delete"}, true},
		{"DeleteInAnotherOU", authzOtherOU, []string{"system:app:delete"}, false},
		{"CreateCannotDelete", authzOwnOU, []string{"system:app:create"}, false},
		{"UpdateCannotDelete", authzOwnOU, []string{"system:app:update"}, false},
		{"ViewCannotDelete", authzOwnOU, []string{"system:app:view"}, false},
		{"MissingEntityIsRootOnly", "", []string{"system:app"}, false},
		{"RootInAnotherOU", authzOtherOU, []string{"system"}, true},
		{"RootWithMissingEntity", "", []string{"system"}, true},
	}
	for _, tc := range cases {
		suite.Run(tc.name, func() {
			service, mockStore := suite.setupAuthzTestService()
			runs := suite.recordSystemFlows(service, nil, nil)
			if tc.appOU != "" {
				resetEntityServiceMethod(service, "GetEntityProfile").
					On("GetEntityProfile", mock.Anything, testServiceAppID).
					Return(&providers.Entity{
						ID: testServiceAppID, Category: providers.EntityCategoryApp, OUID: tc.appOU,
					}, nil)
			}
			mockStore.On("DeleteInboundClient", mock.Anything, testServiceAppID).Maybe().Return(nil)
			resetEntityServiceMethod(service, "DeleteEntity").On("DeleteEntity", mock.Anything, testServiceAppID).
				Maybe().Return(entity.ErrEntityNotFound)

			svcErr := service.DeleteApplication(callerIn(authzOwnOU, tc.permissions...), testServiceAppID)

			if tc.allowed {
				assert.Nil(suite.T(), svcErr)
			} else {
				suite.assertUnauthorized(svcErr)
				mockStore.AssertNotCalled(suite.T(), "DeleteInboundClient", mock.Anything, mock.Anything)
				assert.Empty(suite.T(), *runs, "a refused delete must revoke nothing")
			}
		})
	}
}

func (suite *ServiceTestSuite) TestAuthz_GetApplicationList_ConfinedToTheCallersOU() {
	service, mockStore := suite.setupAuthzTestService()
	resetEntityServiceMethod(service, "GetEntityListCountByOUIDs").
		On("GetEntityListCountByOUIDs", mock.Anything, providers.EntityCategoryApp, []string{authzOwnOU},
			mock.Anything).
		Return(1, nil)
	resetEntityServiceMethod(service, "GetEntityListByOUIDs").
		On("GetEntityListByOUIDs", mock.Anything, providers.EntityCategoryApp, []string{authzOwnOU},
			mock.Anything, 0, mock.Anything).
		Return([]providers.Entity{{ID: "own-app", Category: providers.EntityCategoryApp, OUID: authzOwnOU}}, nil)
	mockStore.On("GetInboundClientList", mock.Anything).
		Return([]inboundmodel.InboundClient{{ID: "own-app"}, {ID: "other-app"}}, nil)

	result, svcErr := service.GetApplicationList(callerIn(authzOwnOU, "system:app:view"), nil)

	suite.Require().Nil(svcErr)
	assert.Equal(suite.T(), 1, result.TotalResults)
	suite.Require().Len(result.Applications, 1)
	assert.Equal(suite.T(), "own-app", result.Applications[0].ID)
}

func (suite *ServiceTestSuite) TestAuthz_GetApplicationList_RootListsEveryOU() {
	service, mockStore := suite.setupAuthzTestService()
	resetEntityServiceMethod(service, "GetEntityListCount").
		On("GetEntityListCount", mock.Anything, providers.EntityCategoryApp, mock.Anything).Return(2, nil)
	resetEntityServiceMethod(service, "GetEntityList").
		On("GetEntityList", mock.Anything, providers.EntityCategoryApp, mock.Anything, 0, mock.Anything).
		Return([]providers.Entity{
			{ID: "own-app", Category: providers.EntityCategoryApp, OUID: authzOwnOU},
			{ID: "other-app", Category: providers.EntityCategoryApp, OUID: authzOtherOU},
		}, nil)
	mockStore.On("GetInboundClientList", mock.Anything).
		Return([]inboundmodel.InboundClient{{ID: "own-app"}, {ID: "other-app"}}, nil)

	result, svcErr := service.GetApplicationList(callerIn(authzOwnOU, "system"), nil)

	suite.Require().Nil(svcErr)
	assert.Len(suite.T(), result.Applications, 2)
}

func (suite *ServiceTestSuite) TestAuthz_GetApplicationList_AnotherOUFilterIsEmpty() {
	service, _ := suite.setupAuthzTestService()

	result, svcErr := service.GetApplicationList(callerIn(authzOwnOU, "system:app:view"),
		map[string]interface{}{"ouId": authzOtherOU})

	suite.Require().Nil(svcErr)
	assert.Zero(suite.T(), result.TotalResults)
	assert.Empty(suite.T(), result.Applications)
}

func (suite *ServiceTestSuite) TestAuthz_GetApplicationList_WithoutPermissionIsEmpty() {
	service, _ := suite.setupAuthzTestService()

	result, svcErr := service.GetApplicationList(callerIn(authzOwnOU, "system:user"), nil)

	suite.Require().Nil(svcErr)
	assert.Empty(suite.T(), result.Applications)
}

func (suite *ServiceTestSuite) TestAuthz_SharingPolicies() {
	service, mockStore := suite.setupAuthzTestService()
	mockLoadFullApplication(mockStore, service,
		&model.ApplicationProcessedDTO{ID: testServiceAppID, Name: "App", OUID: authzOwnOU})
	mockSharing := service.sharingService.(*sharingmock.SharingServiceInterfaceMock)
	mockSharing.EXPECT().GetPolicyList(mock.Anything, ApplicationSharingType, testServiceAppID, 10, 0).
		Maybe().Return(sharing.PolicyList{}, nil)

	viewer := callerIn(authzOwnOU, "system:app:view")
	_, svcErr := service.ListSharingPolicies(viewer, testServiceAppID, 10, 0)
	assert.Nil(suite.T(), svcErr, "a viewer reads the application's sharing policies")

	_, svcErr = service.CreateSharingPolicy(viewer, testServiceAppID, sharing.PolicyRequest{})
	suite.assertUnauthorized(svcErr)
	_, svcErr = service.UpdateSharingPolicy(viewer, testServiceAppID, "policy-1", sharing.PolicyRequest{})
	suite.assertUnauthorized(svcErr)
	suite.assertUnauthorized(service.DeleteSharingPolicy(viewer, testServiceAppID, "policy-1"))

	_, svcErr = service.ListSharingPolicies(callerIn(authzOtherOU, "system:app"), testServiceAppID, 10, 0)
	suite.assertUnauthorized(svcErr)
}

func (suite *ServiceTestSuite) TestAuthz_GetApplicationListByProject_ConfinedToTheCallersOU() {
	for name, tc := range map[string]struct {
		filters map[string]interface{}
		ouIDs   []string
	}{
		"the whole project lists the caller's organization unit": {ouIDs: []string{authzOwnOU}},
		"the caller's organization unit": {
			filters: map[string]interface{}{"ouId": authzOwnOU}, ouIDs: []string{authzOwnOU},
		},
	} {
		suite.Run(name, func() {
			service, mockStore := suite.setupAuthzTestService()
			resetEntityServiceMethod(service, "GetEntityListCountByProject").
				On("GetEntityListCountByProject", mock.Anything, providers.EntityCategoryApp, testProjectID, tc.ouIDs).
				Return(1, nil)
			resetEntityServiceMethod(service, "GetEntityListByProject").
				On("GetEntityListByProject", mock.Anything, providers.EntityCategoryApp, testProjectID, tc.ouIDs,
					mock.Anything, 0).
				Return([]providers.Entity{
					{ID: "own-app", Category: providers.EntityCategoryApp, OUID: authzOwnOU},
				}, nil)
			mockStore.On("GetInboundClientList", mock.Anything).
				Return([]inboundmodel.InboundClient{{ID: "own-app"}}, nil)

			result, svcErr := service.GetApplicationListByProject(
				callerIn(authzOwnOU, "system:app:view"), testProjectID, tc.filters)

			suite.Require().Nil(svcErr)
			suite.Require().Len(result.Applications, 1)
			assert.Equal(suite.T(), "own-app", result.Applications[0].ID)
		})
	}
}

func (suite *ServiceTestSuite) TestAuthz_GetApplicationListByProject_AnotherOUFilterIsEmpty() {
	service, _ := suite.setupAuthzTestService()

	result, svcErr := service.GetApplicationListByProject(callerIn(authzOwnOU, "system:app:view"),
		testProjectID, map[string]interface{}{"ouId": authzOtherOU})

	suite.Require().Nil(svcErr)
	assert.Zero(suite.T(), result.TotalResults)
	assert.Empty(suite.T(), result.Applications)
}
