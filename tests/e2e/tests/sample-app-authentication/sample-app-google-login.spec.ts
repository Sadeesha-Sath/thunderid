/* eslint-disable playwright/require-top-level-describe */
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
 * Sample App Google Social Login Tests
 *
 * Tests the "Continue with Google" login flow against a mock Google OIDC server
 * (utils/mock-google-oidc-server.ts), so no real Google account or network access is needed.
 * The backend is redirected to the mock via identity_provider.google_base_url (see
 * backend/internal/idp/utils.go), configured for local runs by run-e2e.sh.
 *
 * Test Cases:
 * - TC001: Complete Google login flow
 * - TC002: Logout after a Google login
 * - TC003: Login does not complete when Google denies access
 *
 * Prerequisites (automatically handled):
 * - Sample app running at SAMPLE_APP_URL
 * - The server running at SERVER_URL, with identity_provider.google_base_url pointed at
 *   GOOGLE_MOCK_BASE_URL (see run-e2e.sh)
 * - Mock Google OIDC server (automatically started)
 * - Google authentication flow (automatically created)
 *
 * Required environment variables:
 * - SAMPLE_APP_URL: URL of the sample app (e.g., https://localhost:3000)
 * - SERVER_URL: URL of the server (default: https://localhost:8090)
 * - GOOGLE_MOCK_BASE_URL: Base URL the mock Google OIDC server listens on
 *   (default: http://localhost:8093); the suite is skipped when this is not set
 * - ADMIN_USERNAME / ADMIN_PASSWORD: Admin credentials (default: admin/admin)
 */

import { test, expect } from "../../fixtures/sample-app";
import { MockGoogleOIDCServer } from "../../utils/mock-google-oidc-server";
import { GoogleSocialLoginSetup, GoogleSetupResult } from "../../utils/server-setup";
import { Timeouts } from "../../constants/timeouts";
import type { SampleAppLoginPage } from "../../pages/sample-app";
import { request as playwrightRequest } from "@playwright/test";

const sampleAppUrl = process.env.SAMPLE_APP_URL;
const serverUrl = process.env.SERVER_URL || "https://localhost:8090";
// Raw (no fallback) so the skip check below reflects whether the backend is actually wired up
// (identity_provider.google_base_url), not just whether this constant has a usable value.
const googleMockBaseUrlRaw = process.env.GOOGLE_MOCK_BASE_URL;
const googleMockBaseUrl = googleMockBaseUrlRaw || "http://localhost:8093";
const googleMockPort = Number(new URL(googleMockBaseUrl).port || "8093");

const mockClientId = "e2e-mock-google-client-id";
const mockClientSecret = "e2e-mock-google-client-secret";
const mockUser = {
  sub: "e2e-google-user-id",
  email: "e2e-google-user@example.com",
  emailVerified: true,
  name: "E2E Google User",
  givenName: "E2E",
  familyName: "Google User",
};

// The Google IDP must redirect back to the gate app's own callback route (not the sample app's
// URL): the flow's executionId is resumed from sessionStorage on the gate's origin, which a
// cross-origin landing on the sample app can't read. See getGateCallbackUrl() in
// frontend/packages/contexts/src/Config/ConfigProvider.tsx, which the console's connection
// wizard uses to prefill this same field (read-only there, since it's always this fixed path).
const gateCallbackUrl = `${serverUrl.replace(/\/+$/, "")}/gate/callback`;

function buildSetupConfig(request: import("@playwright/test").APIRequestContext) {
  return new GoogleSocialLoginSetup(request, {
    serverUrl,
    applicationClientId: "REACT_SDK_SAMPLE",
    clientId: mockClientId,
    clientSecret: mockClientSecret,
    redirectUri: gateCallbackUrl,
    linkedUser: {
      username: "e2e-google-login-user",
      email: mockUser.email,
      sub: mockUser.sub,
    },
  });
}

/**
 * Drive the sample app through "Continue with Google", waiting for the flow-execute response
 * that carries the final assertion rather than a post-login UI redirect - the browser round-trips
 * through the mock's authorize endpoint and back, and network-level assertion is robust to
 * exactly when the SPA finishes re-rendering (see sample-app-mfa-login.spec.ts TC003 for the
 * same rationale).
 */
async function loginWithGoogle(sampleAppLoginPage: SampleAppLoginPage, page: import("@playwright/test").Page) {
  await sampleAppLoginPage.goto(sampleAppUrl!);
  await sampleAppLoginPage.verifyHomePageLoaded();
  await sampleAppLoginPage.clickSignInButton();
  await sampleAppLoginPage.verifyLoginPageLoaded();

  const [completionResponse] = await Promise.all([
    page.waitForResponse(
      async resp => {
        if (!resp.url().includes("/flow/execute") || resp.request().method() !== "POST") return false;
        try {
          const body = await resp.json();
          return body.flowStatus === "COMPLETE";
        } catch {
          return false;
        }
      },
      { timeout: Timeouts.REDIRECT }
    ),
    sampleAppLoginPage.clickContinueWithGoogle(),
  ]);

  return completionResponse;
}

const describeOrSkip = sampleAppUrl && googleMockBaseUrlRaw ? test.describe : test.describe.skip;

describeOrSkip("Sample App - Google Social Login", () => {
  let mockGoogleServer: MockGoogleOIDCServer;
  let setupResult: GoogleSetupResult | null = null;

  test.beforeAll(async ({ request }) => {
    console.log("\n=== Google Social Login Test Suite Setup ===");

    mockGoogleServer = new MockGoogleOIDCServer(googleMockPort, mockClientId, mockClientSecret);
    mockGoogleServer.addUser(mockUser);
    await mockGoogleServer.start();
    console.log(`✓ Mock Google OIDC Server started at ${mockGoogleServer.getURL()}`);

    const setup = buildSetupConfig(request);

    try {
      setupResult = await setup.setup();
      console.log("✓ Automated setup completed successfully");
    } catch (error) {
      console.error("✗ Automated setup failed:", error);
      throw error;
    }

    console.log("=============================================\n");
  });

  test.afterAll(async () => {
    console.log("\n=== Google Social Login Test Suite Teardown ===");

    if (setupResult) {
      const setup = buildSetupConfig(await playwrightRequest.newContext({ ignoreHTTPSErrors: true }));
      await setup.cleanup(setupResult.cleanupFunctions);
    }

    if (mockGoogleServer) {
      await mockGoogleServer.stop();
      console.log("✓ Mock Google OIDC Server stopped");
    }

    console.log("================================================\n");
  });

  test("TC001: Complete Google login flow", async ({ sampleAppLoginPage, page }) => {
    console.log("\n--- TC001: Google Login ---");

    const completionResponse = await loginWithGoogle(sampleAppLoginPage, page);
    const body = await completionResponse.json();

    expect(body.flowStatus, "the flow must report COMPLETE once Google login finishes").toBe("COMPLETE");
    expect(body.assertion, "a completed flow must carry an assertion").toBeTruthy();
    console.log("✓ Flow completed with an assertion");

    await sampleAppLoginPage.verifyLoggedIn();
    console.log("✓ Google login successful - User logged in");

    console.log("\n--- TC001 Completed Successfully ---\n");
  });

  test("TC002: Logout after a Google login", async ({ sampleAppLoginPage, page }) => {
    console.log("\n--- TC002: Logout after Google Login ---");

    await loginWithGoogle(sampleAppLoginPage, page);
    await sampleAppLoginPage.verifyLoggedIn();
    console.log("✓ Logged in via Google");

    await sampleAppLoginPage.logout();
    await sampleAppLoginPage.verifyLoggedOut();
    console.log("✓ Logged out successfully");

    console.log("\n--- TC002 Completed Successfully ---\n");
  });

  test("TC003: Login does not complete when Google denies access", async ({ sampleAppLoginPage, page }) => {
    console.log("\n--- TC003: Google Access Denied ---");

    mockGoogleServer.setAuthorizeError("access_denied");

    await sampleAppLoginPage.goto(sampleAppUrl!);
    await sampleAppLoginPage.verifyHomePageLoaded();
    await sampleAppLoginPage.clickSignInButton();
    await sampleAppLoginPage.verifyLoginPageLoaded();
    await sampleAppLoginPage.clickContinueWithGoogle();

    // The mock redirects straight back with an OAuth error and no code, so the flow never
    // completes - the gate surfaces an explicit sign-in-failed error instead of logging in.
    const errorLocator = page.locator('.MuiAlert-colorError, [role="alert"]');
    await expect(errorLocator, "an error must be shown when Google denies access").toBeVisible({
      timeout: Timeouts.REDIRECT,
    });
    console.log("✓ Access denied - an error was shown and login did not complete");

    console.log("\n--- TC003 Completed Successfully ---\n");
  });
});
