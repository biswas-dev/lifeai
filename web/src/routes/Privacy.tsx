import { Link } from "react-router-dom";

export function Privacy() {
  return (
    <main className="mx-auto max-w-3xl px-6 py-12 sm:py-20">
      <Link to="/" className="text-2xl font-semibold">lifeai.</Link>
      <p className="eyebrow mt-14">Your information</p>
      <h1 className="mt-4 text-4xl font-semibold tracking-tight">Privacy policy</h1>
      <p className="mt-3 text-sm text-ink-500">Updated September 5, 2026</p>
      <div className="mt-10 space-y-8 text-sm leading-7 text-ink-400">
        <section>
          <h2 className="mb-2 text-lg font-semibold text-ink-100">What Lifeai stores</h2>
          <p>Lifeai is operated by Anshuman Biswas. It stores your account details and the records you choose to add: profile information, meals, photos, recipes, activity, body measurements, journals, and blood reports. Connected services and imported files can add records to your account. We use these records to provide your personal tracker, history, and analysis.</p>
        </section>
        <section>
          <h2 className="mb-2 text-lg font-semibold text-ink-100">Google sign-in</h2>
          <p>Google sign-in requests your basic profile and email address. Lifeai uses your Google account identifier, verified email, name, and profile picture to create or recognize your account. Lifeai never receives your Google password. Signing in does not grant access to Gmail, Google Drive, Calendar, or Google health data. You can remove Lifeai’s access in your Google Account settings.</p>
        </section>
        <section>
          <h2 className="mb-2 text-lg font-semibold text-ink-100">Analysis and connected tools</h2>
          <p>When you use AI features, relevant records or images may be sent to the configured AI service providers, including DeepSeek and NVIDIA. Meal photos can be analyzed automatically after upload, and reports that cannot be read by the built-in parser may use AI extraction. Stored-record summaries do not require an AI request.</p>
          <p className="mt-3">Connecting a service authorizes its records to be imported. Granting an MCP or API token lets the tool holding that token access the permitted records. Read and write permissions are separate, and you can revoke tokens in Settings. Data passed to a connected tool is also subject to that tool’s own privacy practices.</p>
        </section>
        <section>
          <h2 className="mb-2 text-lg font-semibold text-ink-100">Storage and service providers</h2>
          <p>Your records are stored on the servers that run Lifeai. Hosting, network delivery, authentication, and enabled AI providers process information needed to provide their services. Service logs may contain request information such as IP addresses and timestamps. Lifeai does not display your personal health records on the public landing page or sell your records to advertisers.</p>
          <p className="mt-3">Lifeai uses HTTPS, password hashes, account access checks, and encrypted stored integration credentials. Your browser stores a session token so you can stay signed in, and a temporary cookie protects Google sign-in.</p>
        </section>
        <section>
          <h2 className="mb-2 text-lg font-semibold text-ink-100">Your choices and requests</h2>
          <p>You can edit or remove records using the available controls and disconnect imports in Settings. Disconnect a source before removing imported records if you do not want the next sync to restore them. Revoking Google access does not itself delete your Lifeai account.</p>
          <p className="mt-3">For account deletion, a copy of your information, or privacy questions, contact <a className="text-vital-500 underline" href="mailto:anshuman@anshumanbiswas.com">anshuman@anshumanbiswas.com</a>. We may ask you to verify account ownership. Deletion requests include a review of retained files and backups; removing a visible record may not immediately remove all backup copies.</p>
        </section>
        <section>
          <h2 className="mb-2 text-lg font-semibold text-ink-100">Updates</h2>
          <p>This page will be updated when Lifeai’s data practices change. The date above identifies the current version.</p>
        </section>
      </div>
      <Link to="/" className="btn-ghost mt-12">Back to Lifeai</Link>
    </main>
  );
}
