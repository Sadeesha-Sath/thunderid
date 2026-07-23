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

import { Page, Locator, expect } from "@playwright/test";
import { ConsoleRoutes } from "../../configs/routes/console-routes";
import { BasePage } from "../base.page";
import { Timeouts } from "../../constants/timeouts";

export type GoogleConnectionFormData = {
  clientId: string;
  clientSecret: string;
  scopes?: string;
};

/**
 * Page Object Model for the Connections feature (console admin UI).
 *
 * Branded vendors (Google, GitHub, ...) are singletons configured via a one-step
 * "configure" wizard with a fixed name; the connection name field is not rendered for them
 * (see frontend/packages/configure-connections/src/config/connectionVendorMeta.tsx,
 * `presentation: 'branded'`). The redirect URI field is server-derived and read-only.
 */
export class ConnectionsPage extends BasePage {
  readonly baseUrl: string;

  // List page
  readonly connectionsList: Locator;

  // Configure/create wizard (branded vendor, e.g. google)
  readonly clientIdInput: Locator;
  readonly clientSecretInput: Locator;
  readonly redirectUriField: Locator;
  readonly scopesInput: Locator;
  readonly wizardCreateButton: Locator;
  readonly errorAlert: Locator;

  // Detail page
  readonly connectionForm: Locator;
  readonly secretReplaceButton: Locator;
  readonly saveButton: Locator;
  readonly deleteButton: Locator;
  readonly deleteConfirmButton: Locator;

  constructor(page: Page, baseUrl: string) {
    super(page);
    this.baseUrl = baseUrl;

    this.connectionsList = page.locator('[data-testid="connections-list"]');

    this.clientIdInput = page.locator("#connection-field-clientId");
    this.clientSecretInput = page.locator("#connection-field-clientSecret");
    this.redirectUriField = page.locator("#connection-field-redirectUri");
    this.scopesInput = page.locator("#connection-field-scopes");
    this.wizardCreateButton = page.locator('[data-testid="wizard-create"]');
    this.errorAlert = page.locator('[role="alert"].MuiAlert-standardError, [role="alert"].MuiAlert-filledError');

    this.connectionForm = page.locator('[data-testid="connection-form"]');
    this.secretReplaceButton = page.locator('[data-testid="connection-field-clientSecret-replace"]');
    // The UnsavedChangesBar's save action is labeled "Save changes", not a bare "Save".
    this.saveButton = page.getByRole("button", { name: /save changes/i });
    this.deleteButton = page.locator('[data-testid="connection-delete-button"]');
    this.deleteConfirmButton = page.locator('[data-testid="connection-delete-confirm"]');
  }

  /** Navigate to the connections list page */
  async goto(): Promise<void> {
    await this.page.goto(`${this.baseUrl}${ConsoleRoutes.connections}`, {
      waitUntil: "networkidle",
      timeout: Timeouts.PAGE_LOAD,
    });
  }

  /** Navigate directly to a branded vendor's configure (create) wizard, e.g. type="google" */
  async gotoConfigure(type: string): Promise<void> {
    await this.page.goto(`${this.baseUrl}${ConsoleRoutes.connectionConfigure(type)}`, {
      waitUntil: "networkidle",
      timeout: Timeouts.PAGE_LOAD,
    });
  }

  /** Navigate directly to a connection's detail page */
  async gotoDetails(type: string, id: string): Promise<void> {
    await this.page.goto(`${this.baseUrl}${ConsoleRoutes.connectionDetails(type, id)}`, {
      waitUntil: "networkidle",
      timeout: Timeouts.PAGE_LOAD,
    });
  }

  /** Fill the branded-vendor configure form (clientId/clientSecret/optional scopes) */
  async fillOAuthForm(data: GoogleConnectionFormData): Promise<void> {
    await this.clientIdInput.waitFor({ state: "visible", timeout: Timeouts.DEFAULT_ACTION });
    await this.clientIdInput.fill(data.clientId);
    await this.clientSecretInput.fill(data.clientSecret);
    if (data.scopes) {
      await this.scopesInput.fill(data.scopes);
    }
  }

  /**
   * Submit the configure/create wizard and wait for navigation to the detail page.
   * The last path segment must not be "configure" - that also matches `[^/]+` and is the
   * wizard's own URL, so a bare pattern would resolve immediately without waiting for the
   * post-submit navigation.
   */
  async submitCreate(): Promise<void> {
    await this.wizardCreateButton.click();
    await this.page.waitForURL(/\/console\/connections\/[^/]+\/(?!configure$)[^/]+$/, {
      timeout: Timeouts.DEFAULT_ACTION,
    });
  }

  /** Read the connection id from the current detail page URL */
  getConnectionIdFromUrl(): string {
    const match = this.page.url().match(/\/connections\/[^/]+\/([^/?#]+)/);
    if (!match) {
      throw new Error(`Could not extract connection id from URL: ${this.page.url()}`);
    }
    return match[1];
  }

  /** Replace the stored client secret on the detail/edit page, then save */
  async updateClientSecret(newSecret: string): Promise<void> {
    await this.secretReplaceButton.click();
    await this.clientSecretInput.fill(newSecret);
    await this.save();
  }

  /** Update the scopes field on the detail/edit page, then save */
  async updateScopes(scopes: string): Promise<void> {
    await this.scopesInput.waitFor({ state: "visible", timeout: Timeouts.DEFAULT_ACTION });
    await this.scopesInput.fill(scopes);
    await this.save();
  }

  /** Click Save changes and wait for the unsaved-changes bar to clear (success). */
  private async save(): Promise<void> {
    await expect(this.saveButton).toBeEnabled({ timeout: Timeouts.DEFAULT_ACTION });
    await this.saveButton.click();
    await expect(this.saveButton).toBeHidden({ timeout: Timeouts.DEFAULT_ACTION });
  }

  /** Delete the connection via the danger-zone button and confirm dialog */
  async delete(): Promise<void> {
    await this.deleteButton.click();
    await this.deleteConfirmButton.waitFor({ state: "visible", timeout: Timeouts.DEFAULT_ACTION });
    await this.deleteConfirmButton.click();
    await this.page.waitForURL(new RegExp(ConsoleRoutes.connections.replace(/\//g, "\\/") + "$"), {
      timeout: Timeouts.DEFAULT_ACTION,
    });
  }

  /**
   * Locator for a configured connection's card on the list page. The card id rendered by
   * buildConnectionCards.tsx is `${vendorKey}:${instanceId}`, not the bare connection id.
   */
  cardById(type: string, connectionId: string): Locator {
    return this.page.locator(`[data-testid="connection-card-${type}:${connectionId}"]`);
  }
}
