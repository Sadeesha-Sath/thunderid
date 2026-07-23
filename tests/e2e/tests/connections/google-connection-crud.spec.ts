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
 * Google Connection CRUD E2E Tests
 *
 * Tests create/read/update/delete of a Google connection (identity provider) through the
 * console admin UI. Google is a "branded" vendor (see
 * frontend/packages/configure-connections/src/config/connectionVendorMeta.tsx):
 * it is configured via a single-step wizard at /connections/google/configure, its name is
 * fixed to "Google" (not user-editable), and its redirect URI is server-derived and read-only.
 *
 * Test Cases:
 * - TC001: Create a Google connection
 * - TC002: Read the created connection's details (secret stays masked)
 * - TC003: Update the connection's scopes
 * - TC004: Delete the connection
 * - TC005: Create is blocked when required fields are missing
 *
 * Required environment variables:
 * - BASE_URL / SERVER_URL: Console/server base URL
 * - ADMIN_USERNAME / ADMIN_PASSWORD: Admin credentials
 */

import { test, expect } from "../../fixtures/console";
import { getAdminToken } from "../../utils/authentication";
import { TestDataFactory } from "../../utils/test-data";

const serverUrl = process.env.SERVER_URL || "https://localhost:8090";

/** Delete every existing "Google" connection, so a crashed previous run can't collide with TC001. */
async function deleteExistingGoogleConnections(request: import("@playwright/test").APIRequestContext): Promise<void> {
  const adminToken = await getAdminToken(request);
  const response = await request.get(`${serverUrl}/connections/google`, {
    headers: { Authorization: `Bearer ${adminToken}` },
    ignoreHTTPSErrors: true,
  });
  if (!response.ok()) return;

  const existing = await response.json();
  for (const connection of existing ?? []) {
    await request.delete(`${serverUrl}/connections/google/${connection.id}`, {
      headers: { Authorization: `Bearer ${adminToken}` },
      ignoreHTTPSErrors: true,
    });
    console.log(`✓ Removed leftover Google connection: ${connection.id}`);
  }
}

// Tests share a single created connection's id across the CRUD lifecycle, so they must run
// in declaration order rather than Playwright's default (parallelizable-within-file) order.
test.describe.serial("Google Connection - CRUD Operations", () => {
  let connectionId: string | null = null;
  const clientId = TestDataFactory.generateUniqueId("google-client");
  const clientSecret = "e2e-google-client-secret";

  test.beforeAll(async ({ request }) => {
    await deleteExistingGoogleConnections(request);
  });

  test.afterAll(async ({ request }) => {
    // Safety net: if a test failed before TC004 could delete it, clean up here.
    if (connectionId) {
      await deleteExistingGoogleConnections(request);
    }
  });

  test("TC001: Create a Google connection", async ({ connectionsPage }) => {
    await test.step("Navigate to the Google configure wizard", async () => {
      await connectionsPage.gotoConfigure("google");
    });

    await test.step("Fill in client credentials", async () => {
      await connectionsPage.fillOAuthForm({ clientId, clientSecret });
    });

    await test.step("Submit and land on the connection's detail page", async () => {
      await connectionsPage.submitCreate();
      connectionId = connectionsPage.getConnectionIdFromUrl();
      expect(connectionId, "connection id should be present in the detail page URL").toBeTruthy();
    });

    await test.step("Verify the new connection appears in the list", async () => {
      await connectionsPage.goto();
      await expect(connectionsPage.cardById("google", connectionId!)).toBeVisible();
    });
  });

  test("TC002: Read the created connection's details", async ({ connectionsPage }) => {
    expect(connectionId, "TC001 must have created a connection").toBeTruthy();

    await connectionsPage.gotoDetails("google", connectionId!);

    await expect(connectionsPage.clientIdInput).toHaveValue(clientId);
    await expect(connectionsPage.redirectUriField).not.toHaveValue("");
    // The stored secret is never re-sent to the client - it renders as a masked, disabled field.
    await expect(connectionsPage.clientSecretInput).toBeDisabled();
    await expect(connectionsPage.clientSecretInput).not.toHaveValue(clientSecret);
  });

  test("TC003: Update the connection's scopes", async ({ connectionsPage }) => {
    expect(connectionId, "TC001 must have created a connection").toBeTruthy();

    await connectionsPage.gotoDetails("google", connectionId!);
    await connectionsPage.updateScopes("openid email");

    // Reload and verify the change persisted server-side, not just in local component state.
    await connectionsPage.gotoDetails("google", connectionId!);
    await expect(connectionsPage.scopesInput).toHaveValue("openid email");
  });

  test("TC004: Delete the connection", async ({ connectionsPage }) => {
    expect(connectionId, "TC001 must have created a connection").toBeTruthy();

    await connectionsPage.gotoDetails("google", connectionId!);
    await connectionsPage.delete();

    await expect(connectionsPage.cardById("google", connectionId!)).toHaveCount(0);
    connectionId = null;
  });

  test("TC005: Create is blocked when required fields are missing", async ({ connectionsPage }) => {
    await connectionsPage.gotoConfigure("google");

    // No clientId/clientSecret filled in - the create button must stay disabled.
    await expect(connectionsPage.wizardCreateButton).toBeDisabled();

    await connectionsPage.clientIdInput.fill(TestDataFactory.generateUniqueId("google-client"));
    // clientSecret still empty - still required.
    await expect(connectionsPage.wizardCreateButton).toBeDisabled();
  });
});
