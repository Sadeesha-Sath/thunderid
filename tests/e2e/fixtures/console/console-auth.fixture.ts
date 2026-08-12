// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

/**
 * Console Authentication Fixture
 *
 * Provides an `authenticatedPage` fixture that automatically ensures
 * the test page is authenticated before the test runs.
 *
 * This replaces the manual `setupAuthentication` call in beforeEach.
 *
 * @example
 * import { test } from '../fixtures';
 *
 * test('authenticated test', async ({ authenticatedPage }) => {
 *   // authenticatedPage is already logged in
 *   await authenticatedPage.goto(routes.users);
 * });
 */

import { test as base, Page } from "@playwright/test";
import { setupAuthentication } from "../../utils/authentication";

const baseUrl = process.env.BASE_URL as string;

type AuthFixtures = {
  authenticatedPage: Page;
};

export const test = base.extend<AuthFixtures>({
  authenticatedPage: async ({ page }, use) => {
    // Setup authentication before test usage
    const debugAuth = process.env.DEBUG_AUTH === "true";
    // Matches auth.setup.ts's AUTH_STATE_FILE override, so this reads back whichever server's
    // session that invocation's own setup project actually wrote.
    await setupAuthentication(page, baseUrl, { debug: debugAuth, authFilePath: process.env.AUTH_STATE_FILE });

    // Provide the authenticated page to the test
    await use(page);
  },
});
