import { api } from "./api";

export function passkeysSupported() {
  return (
    window.isSecureContext &&
    typeof window.PublicKeyCredential !== "undefined" &&
    !!navigator.credentials
  );
}

function decode(value: string): ArrayBuffer {
  const text = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(text, (c) => c.charCodeAt(0)).buffer;
}
function encode(value: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(value)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

type JSONDescriptor = Omit<PublicKeyCredentialDescriptor, "id"> & {
  id: string;
};
type CreationJSON = Omit<
  PublicKeyCredentialCreationOptions,
  "challenge" | "user" | "excludeCredentials"
> & {
  challenge: string;
  user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string };
  excludeCredentials?: JSONDescriptor[];
};
type RequestJSON = Omit<
  PublicKeyCredentialRequestOptions,
  "challenge" | "allowCredentials"
> & { challenge: string; allowCredentials?: JSONDescriptor[] };

function serialized(credential: PublicKeyCredential) {
  const base = {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    clientExtensionResults: credential.getClientExtensionResults(),
    authenticatorAttachment: credential.authenticatorAttachment,
  };
  const response = credential.response;
  if ("attestationObject" in response) {
    const creation = response as AuthenticatorAttestationResponse;
    return {
      ...base,
      response: {
        clientDataJSON: encode(creation.clientDataJSON),
        attestationObject: encode(creation.attestationObject),
        transports: creation.getTransports?.() || [],
      },
    };
  }
  const assertion = response as AuthenticatorAssertionResponse;
  return {
    ...base,
    response: {
      clientDataJSON: encode(assertion.clientDataJSON),
      authenticatorData: encode(assertion.authenticatorData),
      signature: encode(assertion.signature),
      userHandle: assertion.userHandle ? encode(assertion.userHandle) : null,
    },
  };
}

function passkeyError(error: unknown): never {
  if (
    error instanceof DOMException &&
    (error.name === "NotAllowedError" || error.name === "AbortError")
  ) {
    throw new Error(
      "The passkey prompt was closed or timed out. Try again or use another sign-in method.",
    );
  }
  if (error instanceof DOMException && error.name === "InvalidStateError") {
    throw new Error(
      "This passkey is already registered. Use another device or password manager.",
    );
  }
  throw error;
}

export async function registerPasskey(name: string) {
  if (!passkeysSupported())
    throw new Error("Use an up-to-date browser over HTTPS to add a passkey.");
  try {
    const begin = await api.beginPasskeyRegistration();
    const json = begin.options.publicKey as CreationJSON;
    const credential = (await navigator.credentials.create({
      publicKey: {
        ...json,
        challenge: decode(json.challenge),
        user: { ...json.user, id: decode(json.user.id) },
        excludeCredentials: json.excludeCredentials?.map((c) => ({
          ...c,
          id: decode(c.id),
        })),
      },
    })) as PublicKeyCredential | null;
    if (!credential) throw new Error("No passkey was created.");
    return await api.finishPasskeyRegistration(
      begin.challenge,
      serialized(credential),
      name,
    );
  } catch (error) {
    return passkeyError(error);
  }
}

export async function signInWithPasskey() {
  if (!passkeysSupported())
    throw new Error(
      "Use an up-to-date browser over HTTPS to sign in with a passkey.",
    );
  try {
    const begin = await api.beginPasskeyLogin();
    const json = begin.options.publicKey as RequestJSON;
    const credential = (await navigator.credentials.get({
      publicKey: {
        ...json,
        challenge: decode(json.challenge),
        allowCredentials: json.allowCredentials?.map((c) => ({
          ...c,
          id: decode(c.id),
        })),
      },
    })) as PublicKeyCredential | null;
    if (!credential) throw new Error("No passkey was selected.");
    return await api.finishPasskeyLogin(
      begin.challenge,
      serialized(credential),
    );
  } catch (error) {
    return passkeyError(error);
  }
}
