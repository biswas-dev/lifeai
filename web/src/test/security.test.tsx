import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, expect, test, vi } from "vitest";
import { api } from "../lib/api";
import { VerifyMFA } from "../routes/VerifyMFA";
import { SecuritySettings } from "../components/SecuritySettings";

const { acceptToken } = vi.hoisted(() => ({ acceptToken: vi.fn() }));
vi.mock("../state/AuthContext", () => ({
  useAuth: () => ({
    user: { id: 1, email: "owner@example.com" },
    loginWithToken: acceptToken,
  }),
}));
vi.mock("qrcode", () => ({
  default: {
    toDataURL: vi.fn().mockResolvedValue("data:image/png;base64,AA=="),
  },
}));
vi.mock("../lib/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../lib/api")>()),
  api: {
    verifyMFA: vi.fn(),
    securityStatus: vi.fn(),
    authConfig: vi.fn(),
    beginTOTP: vi.fn(),
    confirmTOTP: vi.fn(),
  },
}));
const status = {
  totp_enabled: false,
  totp_available: true,
  recent_authentication: true,
  recovery_codes_remaining: 0,
  passkeys_available: true,
  passkeys: [],
};
beforeEach(() => {
  vi.clearAllMocks();
  sessionStorage.clear();
  vi.mocked(api.securityStatus).mockResolvedValue(status);
  vi.mocked(api.authConfig).mockResolvedValue({
    allow_signup: true,
    google: true,
    github: false,
  });
});

test("a failed second factor never establishes a session, and recovery codes can complete it", async () => {
  vi.mocked(api.verifyMFA).mockRejectedValueOnce(new Error("Code is invalid"));
  render(
    <MemoryRouter
      initialEntries={[
        {
          pathname: "/verify",
          state: { challenge: "browser-bound-challenge" },
        },
      ]}
    >
      <VerifyMFA />
    </MemoryRouter>,
  );
  fireEvent.change(screen.getByLabelText("Authentication code"), {
    target: { value: "123456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Verify and sign in" }));
  await screen.findByText("Code is invalid");
  expect(acceptToken).not.toHaveBeenCalled();
  expect(api.verifyMFA).toHaveBeenCalledWith(
    "browser-bound-challenge",
    "123456",
  );
  fireEvent.click(screen.getByRole("button", { name: "Use a recovery code" }));
  vi.mocked(api.verifyMFA).mockResolvedValueOnce({
    token: "completed-session",
    user: { id: 1 },
  } as never);
  fireEvent.change(screen.getByLabelText("Recovery code"), {
    target: { value: "ABCDE-FGHJK" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Verify and sign in" }));
  await waitFor(() =>
    expect(acceptToken).toHaveBeenCalledWith("completed-session"),
  );
});

test("authenticator setup stays pending until verified and recovery codes require acknowledgment", async () => {
  vi.mocked(api.beginTOTP).mockResolvedValue({
    challenge: "setup-challenge",
    secret: "SETUPKEY",
    uri: "otpauth://totp/Lifeai:test?secret=SETUPKEY",
  });
  vi.mocked(api.confirmTOTP).mockRejectedValueOnce(
    new Error("Enter the current code"),
  );
  render(
    <MemoryRouter>
      <SecuritySettings />
    </MemoryRouter>,
  );
  await screen.findByText("2FA not enabled");
  fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }));
  await screen.findByAltText("Authenticator setup QR code");
  expect(
    screen.queryByText("Save your recovery codes"),
  ).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("Six-digit authentication code"), {
    target: { value: "123456" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Verify and enable 2FA" }),
  );
  await screen.findByText("Enter the current code");
  expect(acceptToken).not.toHaveBeenCalled();
  vi.mocked(api.confirmTOTP).mockResolvedValueOnce({
    token: "new-session",
    user: { id: 1 },
    recovery_codes: ["ABCDE-FGHJK"],
  } as never);
  vi.mocked(api.securityStatus).mockResolvedValue({
    ...status,
    totp_enabled: true,
    recovery_codes_remaining: 10,
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Verify and enable 2FA" }),
  );
  await screen.findByText("Save your recovery codes");
  expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "I’ve saved these codes somewhere safe.",
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(screen.queryByText("ABCDE-FGHJK")).not.toBeInTheDocument();
});

test("stale sessions can read security status but must reauthenticate before enrollment", async () => {
  vi.mocked(api.securityStatus).mockResolvedValue({
    ...status,
    recent_authentication: false,
  });
  render(
    <MemoryRouter>
      <SecuritySettings />
    </MemoryRouter>,
  );
  await screen.findByText("Confirm it’s you");
  expect(
    screen.getByRole("button", { name: "Set up authenticator" }),
  ).toBeDisabled();
  expect(
    screen.getByRole("link", { name: "Verify with Google" }),
  ).toHaveAttribute("href", "/api/auth/google");
});
