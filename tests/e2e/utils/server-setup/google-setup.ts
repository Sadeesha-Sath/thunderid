/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Google Social Login Setup Utilities
 *
 * Automated setup for Google social login E2E testing prerequisites:
 * - Google connection (identity provider) pointing at the mock Google OIDC server
 * - Authentication flow with a "Continue with Google" step (GoogleOIDCAuthExecutor)
 * - Application rewired to use that flow, with its previous flow bindings restored on cleanup
 *
 * Mirrors utils/server-setup/mfa-setup.ts.
 */

import { APIRequestContext, APIResponse, request as playwrightRequest } from "@playwright/test";
import { getAdminToken } from "../authentication";
import googleAuthFlowNodesTemplate from "./google-auth-flow-nodes.json";

export interface GoogleLinkedUser {
  username: string;
  email: string;
  /** Must match the `sub` claim the mock Google OIDC server issues for this identity. */
  sub: string;
}

export interface GoogleSetupConfig {
  serverUrl: string;
  applicationClientId: string;
  clientId: string;
  clientSecret: string;
  redirectUri: string;
  /**
   * Local user to link the federated identity to. The Google auth flow node sets
   * allowAuthenticationWithoutLocalUser: false (matching real usage, where a user first
   * registers via Google and later logs in against that stored identity), so login only
   * succeeds when a local user with this `sub` attribute already exists.
   */
  linkedUser: GoogleLinkedUser;
}

export interface GoogleSetupResult {
  adminToken: string;
  connectionId: string;
  authFlowId: string;
  applicationId: string;
  userId: string;
  cleanupFunctions: Array<() => Promise<void>>;
}

export class GoogleSocialLoginSetup {
  constructor(
    private request: APIRequestContext,
    private config: GoogleSetupConfig
  ) {}

  /**
   * Perform complete Google social login setup
   */
  async setup(): Promise<GoogleSetupResult> {
    console.log("\n=== Google Social Login Setup Started ===");

    const cleanupFunctions: Array<() => Promise<void>> = [];

    try {
      const adminToken = await getAdminToken(this.request);
      console.log("✓ Admin authentication successful");

      const connectionResult = await this.createOrGetGoogleConnection(adminToken);
      if (connectionResult.created) {
        console.log(`✓ Google connection created: ${connectionResult.id}`);
        cleanupFunctions.push(() =>
          this.deleteResource(
            adminToken,
            `${this.config.serverUrl}/connections/google/${connectionResult.id}`,
            connectionResult.id,
            "Google connection"
          )
        );
      } else {
        console.log(`✓ Using existing Google connection: ${connectionResult.id}`);
      }
      const actualConnectionId = connectionResult.id;

      const authFlowResult = await this.createOrGetGoogleAuthFlow(adminToken, actualConnectionId);
      if (authFlowResult.created) {
        console.log(`✓ Google authentication flow created: ${authFlowResult.id}`);
        cleanupFunctions.push(() =>
          this.deleteResource(
            adminToken,
            `${this.config.serverUrl}/flows/${authFlowResult.id}`,
            authFlowResult.id,
            "Flow"
          )
        );
      } else {
        console.log(`✓ Using existing Google authentication flow: ${authFlowResult.id}`);
      }
      const actualAuthFlowId = authFlowResult.id;

      const { appId, originalFlows } = await this.updateApplicationFlow(adminToken, actualAuthFlowId);
      console.log("✓ Application updated with Google authentication flow");
      cleanupFunctions.push(() => this.revertApplicationFlow(adminToken, appId, originalFlows));

      const userResult = await this.createOrGetLinkedUser(adminToken);
      if (userResult.created) {
        console.log(`✓ Linked local user created: ${userResult.id}`);
        cleanupFunctions.push(() =>
          this.deleteResource(
            adminToken,
            `${this.config.serverUrl}/users/${userResult.id}`,
            userResult.id,
            "Linked user"
          )
        );
      } else {
        console.log(`✓ Using existing linked local user: ${userResult.id}`);
      }
      const actualUserId = userResult.id;

      console.log("=== Google Social Login Setup Completed ===\n");

      return {
        adminToken,
        connectionId: actualConnectionId,
        authFlowId: actualAuthFlowId,
        applicationId: appId,
        userId: actualUserId,
        cleanupFunctions,
      };
    } catch (error) {
      console.error("✗ Google Social Login Setup failed:", error);
      await this.cleanup(cleanupFunctions);
      throw error;
    }
  }

  /**
   * Cleanup all created resources
   */
  async cleanup(cleanupFunctions: Array<() => Promise<void>>): Promise<void> {
    console.log("\n=== Google Social Login Cleanup Started ===");

    for (const cleanup of cleanupFunctions.reverse()) {
      try {
        await cleanup();
      } catch (error) {
        console.error("⚠️  Cleanup error (non-fatal):", error);
      }
    }

    console.log("=== Google Social Login Cleanup Completed ===\n");
  }

  /**
   * Create or get an existing Google connection pointing at the mock server's client credentials.
   */
  private async createOrGetGoogleConnection(adminToken: string): Promise<{ id: string; created: boolean }> {
    const name = "E2E Mock Google Connection";

    return this.createOrGet(
      "Google connection",
      () =>
        this.request.post(`${this.config.serverUrl}/connections/google`, {
          data: {
            name,
            description: "Mock Google identity provider for e2e social login testing",
            clientId: this.config.clientId,
            clientSecret: this.config.clientSecret,
            redirectUri: this.config.redirectUri,
            scopes: ["openid", "email", "profile"],
          },
          headers: {
            Authorization: `Bearer ${adminToken}`,
            "Content-Type": "application/json",
          },
          ignoreHTTPSErrors: true,
        }),
      () => this.getExistingGoogleConnection(adminToken, name)
    );
  }

  /**
   * Get existing Google connection by name
   */
  private async getExistingGoogleConnection(adminToken: string, name: string): Promise<string> {
    const response = await this.request.get(`${this.config.serverUrl}/connections/google`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      ignoreHTTPSErrors: true,
    });

    if (!response.ok()) {
      throw new Error(`Failed to fetch Google connections: ${await response.text()}`);
    }

    const data = await response.json();
    const connection = (data.connections ?? data)?.find((c: any) => c.name === name);

    if (!connection) {
      throw new Error(`Google connection '${name}' exists but could not be found in the list`);
    }

    return connection.id;
  }

  /**
   * Create or get an existing authentication flow with a "Continue with Google" step
   */
  private async createOrGetGoogleAuthFlow(
    adminToken: string,
    connectionId: string
  ): Promise<{ id: string; created: boolean }> {
    const flowHandle = "e2e-google-auth-flow";

    return this.createOrGet(
      "Google authentication flow",
      () =>
        this.request.post(`${this.config.serverUrl}/flows`, {
          data: {
            handle: flowHandle,
            name: "E2E Google Authentication Flow",
            flowType: "AUTHENTICATION",
            activeVersion: 2,
            nodes: this.getGoogleFlowNodes(connectionId),
          },
          headers: {
            Authorization: `Bearer ${adminToken}`,
            "Content-Type": "application/json",
          },
          ignoreHTTPSErrors: true,
        }),
      () => this.getExistingFlow(adminToken, flowHandle)
    );
  }

  /**
   * Get existing flow by handle
   */
  private async getExistingFlow(adminToken: string, handle: string): Promise<string> {
    const filterQuery = `handle eq "${handle}"`;

    const response = await this.request.get(
      `${this.config.serverUrl}/flows?filter=${encodeURIComponent(filterQuery)}`,
      {
        headers: { Authorization: `Bearer ${adminToken}` },
        ignoreHTTPSErrors: true,
      }
    );

    if (!response.ok()) {
      throw new Error(`Failed to fetch flows: ${await response.text()}`);
    }

    const data = await response.json();
    const flow = data.flows?.find((f: any) => f.handle === handle);

    if (!flow) {
      throw new Error(`Flow '${handle}' exists but could not be found in the list`);
    }

    return flow.id;
  }

  /**
   * Rewire the target application's authFlowId to the Google authentication flow.
   * recoveryFlowId is cleared for the same reason MFASetup clears it: a leftover recovery
   * flow that calls back into the default authentication flow is rejected as inconsistent
   * once authFlowId points elsewhere. Registration is disabled for the same reason: the
   * backend rejects a registrationFlowId that references a different authFlowId than the
   * one now configured on the application (APP-1039 "Conflicting flow references"), and this
   * setup has no registration flow of its own to keep it pointed at.
   */
  private async updateApplicationFlow(
    adminToken: string,
    authFlowId: string
  ): Promise<{
    appId: string;
    originalFlows: {
      authFlowId: string;
      recoveryFlowId: string | null;
      registrationFlowId: string | null;
      isRegistrationFlowEnabled: boolean;
    };
  }> {
    const listResponse = await this.request.get(`${this.config.serverUrl}/applications`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      ignoreHTTPSErrors: true,
    });

    if (!listResponse.ok()) {
      throw new Error(`Failed to fetch applications: ${await listResponse.text()}`);
    }

    const listData = await listResponse.json();
    const targetApp = listData.applications?.find((app: any) => app.clientId === this.config.applicationClientId);

    if (!targetApp) {
      throw new Error(`Application with clientId "${this.config.applicationClientId}" not found`);
    }

    const actualAppId = targetApp.id;

    const getResponse = await this.request.get(`${this.config.serverUrl}/applications/${actualAppId}`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      ignoreHTTPSErrors: true,
    });

    if (!getResponse.ok()) {
      throw new Error(`Failed to fetch application: ${await getResponse.text()}`);
    }

    const appData = await getResponse.json();
    const originalFlows = {
      authFlowId: appData.authFlowId,
      recoveryFlowId: appData.recoveryFlowId ?? null,
      registrationFlowId: appData.registrationFlowId ?? null,
      isRegistrationFlowEnabled: appData.isRegistrationFlowEnabled ?? false,
    };

    const updatedApp = {
      ...appData,
      authFlowId,
      recoveryFlowId: null,
      registrationFlowId: null,
      isRegistrationFlowEnabled: false,
    };

    const updateResponse = await this.request.put(`${this.config.serverUrl}/applications/${actualAppId}`, {
      data: updatedApp,
      headers: { Authorization: `Bearer ${adminToken}`, "Content-Type": "application/json" },
      ignoreHTTPSErrors: true,
    });

    if (!updateResponse.ok()) {
      throw new Error(`Failed to update application: ${await updateResponse.text()}`);
    }

    return { appId: actualAppId, originalFlows };
  }

  /**
   * Restore the application's flow bindings to what they were before this setup rewired them.
   */
  private async revertApplicationFlow(
    adminToken: string,
    appId: string,
    originalFlows: {
      authFlowId: string;
      recoveryFlowId: string | null;
      registrationFlowId: string | null;
      isRegistrationFlowEnabled: boolean;
    }
  ): Promise<void> {
    // Fresh request context: the beforeAll-scoped `this.request` fixture is dead by afterAll.
    let requestContext: APIRequestContext | null = null;
    try {
      requestContext = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });

      const getResponse = await requestContext.get(`${this.config.serverUrl}/applications/${appId}`, {
        headers: { Authorization: `Bearer ${adminToken}` },
      });

      if (!getResponse.ok()) {
        console.log(`⚠️  Could not fetch application for revert: ${await getResponse.text()}`);
        return;
      }

      const appData = await getResponse.json();
      const revertedApp = { ...appData, ...originalFlows };

      const updateResponse = await requestContext.put(`${this.config.serverUrl}/applications/${appId}`, {
        data: revertedApp,
        headers: { Authorization: `Bearer ${adminToken}`, "Content-Type": "application/json" },
      });

      if (updateResponse.ok()) {
        console.log(`✓ Application flows reverted: ${appId}`);
      } else {
        console.log(`⚠️  Could not revert application flows: ${await updateResponse.text()}`);
      }
    } catch (error) {
      console.log(`⚠️  Error reverting application flows: ${error}`);
    } finally {
      if (requestContext) {
        await requestContext.dispose();
      }
    }
  }

  /**
   * Create or get a local user carrying the `sub` attribute the Google auth flow links against.
   */
  private async createOrGetLinkedUser(adminToken: string): Promise<{ id: string; created: boolean }> {
    const schemasResponse = await this.request.get(`${this.config.serverUrl}/user-types`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      ignoreHTTPSErrors: true,
    });

    if (!schemasResponse.ok()) {
      throw new Error(`Failed to fetch user types: ${await schemasResponse.text()}`);
    }

    const schemasData = await schemasResponse.json();
    const personSchema = schemasData.types?.find((s: any) => s.name === "Person");

    if (!personSchema || !personSchema.ouId) {
      throw new Error("Person user type not found or missing organization unit");
    }

    return this.createOrGet(
      "linked user",
      () =>
        this.request.post(`${this.config.serverUrl}/users`, {
          data: {
            type: "Person",
            ouId: personSchema.ouId,
            attributes: {
              username: this.config.linkedUser.username,
              email: this.config.linkedUser.email,
              sub: this.config.linkedUser.sub,
            },
          },
          headers: { Authorization: `Bearer ${adminToken}`, "Content-Type": "application/json" },
          ignoreHTTPSErrors: true,
        }),
      () => this.getExistingUser(adminToken)
    );
  }

  /**
   * Get existing linked user by username
   */
  private async getExistingUser(adminToken: string): Promise<string> {
    const response = await this.request.get(
      `${this.config.serverUrl}/users?filter=username eq "${this.config.linkedUser.username}"`,
      {
        headers: { Authorization: `Bearer ${adminToken}` },
        ignoreHTTPSErrors: true,
      }
    );

    if (!response.ok()) {
      throw new Error(`Failed to fetch existing linked user: ${await response.text()}`);
    }

    const data = await response.json();
    if (!data.users || data.users.length === 0) {
      throw new Error("Linked user exists but could not be found");
    }

    return data.users[0].id;
  }

  /**
   * Create a resource, or fall back to looking up the existing one when the backend
   * reports it already exists (409, or an error body mentioning "duplicate"/"already exists").
   */
  private async createOrGet(
    resourceLabel: string,
    createFn: () => Promise<APIResponse>,
    findExistingFn: () => Promise<string>
  ): Promise<{ id: string; created: boolean }> {
    const response = await createFn();

    if (response.ok()) {
      const data = await response.json();
      return { id: data.id, created: true };
    }

    const errorText = await response.text();
    if (response.status() === 409 || errorText.includes("duplicate") || errorText.includes("already exists")) {
      return { id: await findExistingFn(), created: false };
    }

    throw new Error(`Failed to create ${resourceLabel}: ${errorText}`);
  }

  /**
   * Delete a resource via a fresh request context (the beforeAll-scoped `this.request`
   * fixture is dead by afterAll), logging success/failure without throwing.
   */
  private async deleteResource(adminToken: string, url: string, resourceId: string, label: string): Promise<void> {
    let requestContext: APIRequestContext | null = null;
    try {
      requestContext = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });

      const response = await requestContext.delete(url, {
        headers: { Authorization: `Bearer ${adminToken}` },
      });

      if (response.ok()) {
        console.log(`✓ ${label} deleted: ${resourceId}`);
      } else {
        console.log(`⚠️  Could not delete ${label}: ${await response.text()}`);
      }
    } catch (error) {
      console.log(`⚠️  Error deleting ${label}: ${error}`);
    } finally {
      if (requestContext) {
        await requestContext.dispose();
      }
    }
  }

  /**
   * Get Google auth flow node definitions with the connection id injected
   */
  private getGoogleFlowNodes(connectionId: string): any[] {
    const nodesJson = JSON.stringify(googleAuthFlowNodesTemplate);
    const nodesWithIdpId = nodesJson.replace(/\{\{IDP_ID\}\}/g, connectionId);
    return JSON.parse(nodesWithIdpId);
  }
}
