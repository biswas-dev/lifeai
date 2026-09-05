import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { api } from "../lib/api";
import { registerPasskey, signInWithPasskey } from "../lib/passkeys";

vi.mock("../lib/api", () => ({
  api: {
    beginPasskeyRegistration: vi.fn(),
    finishPasskeyRegistration: vi.fn(),
    beginPasskeyLogin: vi.fn(),
    finishPasskeyLogin: vi.fn(),
  },
}));
const create = vi.fn(),
  get = vi.fn();
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("isSecureContext", true);
  vi.stubGlobal("PublicKeyCredential", class {});
  vi.stubGlobal("navigator", { credentials: { create, get } });
});
afterEach(() => vi.unstubAllGlobals());
const buffer = () => new Uint8Array([1, 2, 3]).buffer;

test("passkey registration converts browser binary fields without losing the challenge", async () => {
  vi.mocked(api.beginPasskeyRegistration).mockResolvedValue({
    challenge: "server-ceremony",
    options: {
      publicKey: {
        challenge: "AQID",
        user: { id: "AQID", name: "test", displayName: "Test" },
        rp: { name: "Lifeai", id: "lifeai.cc" },
        pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        authenticatorSelection: {
          userVerification: "required",
          residentKey: "required",
        },
      },
    },
  });
  create.mockResolvedValue({
    id: "AQID",
    rawId: buffer(),
    type: "public-key",
    getClientExtensionResults: () => ({}),
    response: {
      clientDataJSON: buffer(),
      attestationObject: buffer(),
      getTransports: () => ["internal"],
    },
  });
  await registerPasskey("Personal laptop");
  const sent = create.mock.calls[0][0].publicKey;
  expect(Array.from(new Uint8Array(sent.challenge))).toEqual([1, 2, 3]);
  expect(sent.authenticatorSelection.userVerification).toBe("required");
  expect(api.finishPasskeyRegistration).toHaveBeenCalledWith(
    "server-ceremony",
    expect.objectContaining({
      response: {
        clientDataJSON: "AQID",
        attestationObject: "AQID",
        transports: ["internal"],
      },
    }),
    "Personal laptop",
  );
});

test("cancelled passkey prompts do not submit a credential", async () => {
  vi.mocked(api.beginPasskeyLogin).mockResolvedValue({
    challenge: "login-ceremony",
    options: {
      publicKey: {
        challenge: "AQID",
        rpId: "lifeai.cc",
        userVerification: "required",
      },
    },
  });
  get.mockRejectedValue(new DOMException("Cancelled", "NotAllowedError"));
  await expect(signInWithPasskey()).rejects.toThrow("closed or timed out");
  expect(api.finishPasskeyLogin).not.toHaveBeenCalled();
});
