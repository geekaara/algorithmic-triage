package main

// The Tripline dataset: a four-engineer startup six weeks out from an
// investor demo. Sprints 1–3 are closed (velocity history), Sprint 4 is in
// flight, Sprints 5–6 are empty and are what the triage engine plans into.
//
// The backlog deliberately mixes genuine MVP work, scope creep with a
// "Highest" priority, tickets that need splitting, unestimated work and
// dependency chains, so the engine has real decisions to make.

var sprints = []sprintSpec{
	{Ref: "S1", Name: "TRIP Sprint 1", Goal: "Walking skeleton: deployable app with sign-up and login", Start: "2026-08-24", End: "2026-09-04", State: "closed"},
	{Ref: "S2", Name: "TRIP Sprint 2", Goal: "Create a trip and get the group into it", Start: "2026-09-07", End: "2026-09-18", State: "closed"},
	{Ref: "S3", Name: "TRIP Sprint 3", Goal: "Shared day-by-day itinerary", Start: "2026-09-21", End: "2026-10-02", State: "closed"},
	{Ref: "S4", Name: "TRIP Sprint 4", Goal: "Group decisions and first expenses", Start: "2026-10-05", End: "2026-10-16", State: "active"},
	{Ref: "S5", Name: "TRIP Sprint 5", Goal: "", Start: "2026-10-19", End: "2026-10-30", State: "future"},
	{Ref: "S6", Name: "TRIP Sprint 6 (Demo day)", Goal: "", Start: "2026-11-02", End: "2026-11-13", State: "future"},
}

var epics = []issueSpec{
	{Ref: "E-ACC", Summary: "Accounts & Profiles", Description: "Sign-up, login, password reset, consent and the user's own profile."},
	{Ref: "E-TRIP", Summary: "Trips & Invites", Description: "Creating a trip and getting the group into it."},
	{Ref: "E-ITIN", Summary: "Shared Itinerary", Description: "The day-by-day plan the group builds and votes on together."},
	{Ref: "E-EXP", Summary: "Expense Splitting", Description: "Logging who paid for what and working out who owes whom."},
	{Ref: "E-NOTIF", Summary: "Notifications", Description: "Keeping members informed when the trip changes."},
	{Ref: "E-PLAT", Summary: "Platform & Infrastructure", Description: "Hosting, CI/CD, observability, performance and security."},
	{Ref: "E-BOOK", Summary: "Search & Booking", Description: "Finding and booking flights and hotels from inside Tripline."},
	{Ref: "E-AI", Summary: "Personalisation & AI", Description: "Recommendations and AI-assisted planning."},
	{Ref: "E-GROW", Summary: "Growth & Analytics", Description: "Referral, loyalty and internal analytics."},
}

var issues = []issueSpec{
	// ---- Sprint 1 (closed, 18 pts) ----------------------------------------
	{Ref: "API-SKEL", Type: "Task", Epic: "E-PLAT", Sprint: "S1", Status: "Done", Points: 3, Priority: "High", Labels: []string{"backend"},
		Summary: "Go API service skeleton deployed to Cloud Run",
		Description: `Stand up the Go API with health check, structured logging and graceful shutdown, deployed to Cloud Run.

## Acceptance criteria
- GET /healthz returns 200 with build SHA.
- Logs are JSON and visible in Cloud Logging.
- Service scales to zero when idle.`},
	{Ref: "DB-MIG", Type: "Task", Epic: "E-PLAT", Sprint: "S1", Status: "Done", Points: 3, Priority: "High", Labels: []string{"backend", "database"},
		Summary: "Cloud SQL Postgres with versioned migrations",
		Description: `Provision Cloud SQL Postgres and add a migrations pipeline (golang-migrate) that runs on deploy.

## Acceptance criteria
- Migrations run automatically before the new revision receives traffic.
- Rollback is documented.`},
	{Ref: "CICD", Type: "Task", Epic: "E-PLAT", Sprint: "S1", Status: "Done", Points: 3, Priority: "High", Labels: []string{"devops"},
		Summary:     "CI/CD: build, test and deploy to staging on merge",
		Description: `GitHub Actions pipeline: lint, test, build image, deploy to staging on merge to main. Production deploy is a manual approval.`},
	{Ref: "AUTH", Type: "Story", Epic: "E-ACC", Sprint: "S1", Status: "Done", Points: 5, Priority: "Highest", Labels: []string{"frontend", "backend"},
		Summary: "Sign up and log in with email and password (Firebase Auth)",
		Description: `As a traveller, I want to create an account and log in so that my trips are saved.

## Acceptance criteria
- Email/password sign-up and login via Firebase Auth.
- API verifies Firebase ID tokens on every request.
- Session persists across page reloads.`},
	{Ref: "SHELL", Type: "Task", Epic: "E-PLAT", Sprint: "S1", Status: "Done", Points: 2, Priority: "Medium", Labels: []string{"frontend"},
		Summary:     "Next.js app shell with responsive layout and routing",
		Description: `App shell, navigation and layout primitives, mobile-first. Hosted on Firebase Hosting.`},
	{Ref: "ENVS", Type: "Task", Epic: "E-PLAT", Sprint: "S1", Status: "Done", Points: 2, Priority: "Medium", Labels: []string{"devops"},
		Summary:     "Separate staging and production environments",
		Description: `Separate GCP projects, Firebase projects and secrets for staging and production.`},

	// ---- Sprint 2 (closed, 22 pts) ----------------------------------------
	{Ref: "TRIP-CREATE", Type: "Story", Epic: "E-TRIP", Sprint: "S2", Status: "Done", Points: 3, Priority: "Highest", Labels: []string{"frontend", "backend"},
		Summary: "Create a trip with name, destination and dates",
		Description: `As an organiser, I want to create a trip so that I can start planning with my friends.

## Acceptance criteria
- Name, destination and start/end dates are required.
- The creator becomes the trip organiser.`},
	{Ref: "TRIP-LIST", Type: "Story", Epic: "E-TRIP", Sprint: "S2", Status: "Done", Points: 3, Priority: "High", Labels: []string{"frontend"},
		Summary:     "My trips dashboard",
		Description: `As a user, I want to see all trips I belong to, upcoming first.`},
	{Ref: "INVITE-LINK", Type: "Story", Epic: "E-TRIP", Sprint: "S2", Status: "Done", Points: 5, Priority: "Highest", Labels: []string{"backend"},
		Summary: "Generate a shareable invite link",
		Description: `As an organiser, I want a link I can paste into the group chat so that friends can join.

## Acceptance criteria
- Link is unguessable and expires after 14 days.
- Organiser can revoke and regenerate the link.`},
	{Ref: "JOIN", Type: "Story", Epic: "E-TRIP", Sprint: "S2", Status: "Done", Points: 5, Priority: "Highest", Labels: []string{"frontend", "backend"},
		Summary: "Join a trip from an invite link",
		Description: `As an invited friend, I want to open the link and land in the trip, signing up first if I need to.

## Acceptance criteria
- New users are taken through sign-up and then into the trip.
- Existing members opening the link are not duplicated.`},
	{Ref: "MEMBERS", Type: "Story", Epic: "E-TRIP", Sprint: "S2", Status: "Done", Points: 3, Priority: "Medium", Labels: []string{"frontend", "backend"},
		Summary:     "Trip members list with organiser and member roles",
		Description: `Show who is in the trip. Only organisers can remove members.`},
	{Ref: "BUG-INVITE-404", Type: "Bug", Epic: "E-TRIP", Sprint: "S2", Status: "Done", Points: 1, Priority: "High", Labels: []string{"frontend"},
		Summary:     "Invite link returns 404 when the user is logged out",
		Description: `Steps: log out, open an invite link. Expected: sign-up page. Actual: 404. Route guard runs before the invite handler.`},
	{Ref: "PW-RESET", Type: "Story", Epic: "E-ACC", Sprint: "S2", Status: "Done", Points: 2, Priority: "High", Labels: []string{"frontend"},
		Summary:     "Password reset by email",
		Description: `As a user who forgot my password, I want to reset it by email.`},

	// ---- Sprint 3 (closed, 20 pts) ----------------------------------------
	{Ref: "ITIN-API", Type: "Task", Epic: "E-ITIN", Sprint: "S3", Status: "Done", Points: 5, Priority: "Highest", Labels: []string{"backend", "database"},
		Summary:     "Itinerary data model and API (flights, stays, activities)",
		Description: `Model itinerary items with type, title, start/end time (with time zone), location, link and notes. CRUD endpoints scoped to trip membership.`},
	{Ref: "ITIN-CRUD", Type: "Story", Epic: "E-ITIN", Sprint: "S3", Status: "Done", Points: 5, Priority: "Highest", Labels: []string{"frontend"},
		Summary:     "Add, edit and delete itinerary items",
		Description: `As a trip member, I want to add flights, stays and activities to the shared plan.`},
	{Ref: "TIMELINE", Type: "Story", Epic: "E-ITIN", Sprint: "S3", Status: "Done", Points: 5, Priority: "High", Labels: []string{"frontend"},
		Summary:     "Day-by-day timeline view",
		Description: `As a trip member, I want to see the plan grouped by day in the destination's local time.`},
	{Ref: "REORDER", Type: "Story", Epic: "E-ITIN", Sprint: "S3", Status: "Done", Points: 3, Priority: "Medium", Labels: []string{"frontend"},
		Summary:     "Drag to reorder items within a day",
		Description: `Reordering persists for all members.`},
	{Ref: "BUG-TZ", Type: "Bug", Epic: "E-ITIN", Sprint: "S3", Status: "Done", Points: 2, Priority: "High", Labels: []string{"backend"},
		Summary:     "Itinerary items shift by one day for users in UTC+10",
		Description: `Dates were stored as midnight UTC. Brisbane users saw every item a day early. Store local date plus IANA zone.`},

	// ---- Sprint 4 (active, 21 pts committed) ------------------------------
	{Ref: "VOTE", Type: "Story", Epic: "E-ITIN", Sprint: "S4", Status: "Done", Points: 3, Priority: "High", Labels: []string{"frontend", "backend"},
		Summary: "Vote on itinerary options",
		Description: `As a trip member, I want to upvote or downvote options so that the group can decide.

## Acceptance criteria
- One vote per member per item, changeable.
- Items show net score.`},
	{Ref: "EXP-API", Type: "Task", Epic: "E-EXP", Sprint: "S4", Status: "Done", Points: 3, Priority: "Highest", Labels: []string{"backend", "database"},
		Summary:     "Expense data model and API",
		Description: `Expenses store amount in AUD cents, payer, participants, description and date. Integer arithmetic only.`},
	{Ref: "EXP-LOG", Type: "Story", Epic: "E-EXP", Sprint: "S4", Status: "In Progress", Points: 3, Priority: "Highest", Labels: []string{"frontend"},
		Summary: "Log an expense",
		Description: `As a trip member, I want to record that I paid for something so that it can be split.

## Acceptance criteria
- Amount, description, payer (defaults to me) and date.
- Validation for zero and negative amounts.`},
	{Ref: "EXP-SPLIT", Type: "Story", Epic: "E-EXP", Sprint: "S4", Status: "In Progress", Points: 5, Priority: "Highest", Labels: []string{"backend"},
		Summary: "Split an expense equally between selected members",
		Description: `As a payer, I want to choose who shared the cost so that it is split fairly.

## Acceptance criteria
- Defaults to all members.
- Remainder cents are assigned deterministically so totals always reconcile.`},
	{Ref: "SENTRY", Type: "Task", Epic: "E-PLAT", Sprint: "S4", Status: "Done", Points: 2, Priority: "High", Labels: []string{"devops"},
		Summary:     "Error monitoring for web and API (Sentry)",
		Description: `Capture unhandled errors with release tags and alert on new issues in production.`},
	{Ref: "MOBILE-PASS", Type: "Story", Epic: "E-ITIN", Sprint: "S4", Status: "To Do", Points: 3, Priority: "High", Labels: []string{"frontend"},
		Summary:     "Mobile responsiveness pass on itinerary screens",
		Description: `The demo runs on phones. Fix overflow, tap targets and the timeline on 375px screens.`},
	{Ref: "BUG-VOTE-REFRESH", Type: "Bug", Epic: "E-ITIN", Sprint: "S4", Status: "To Do", Points: 2, Priority: "Medium", Labels: []string{"frontend"},
		Summary:     "Vote counts don't update for other members until page reload",
		Description: `Other members see stale vote counts. Suggested fix in standup was WebSockets (see real-time collaboration ticket). Refetching on focus plus 30s polling would also resolve it.`},

	// ---- Backlog, in stakeholder rank order --------------------------------
	{Ref: "PROFILE-ML", Type: "Story", Epic: "E-AI", Points: 13, Priority: "Highest", Labels: []string{"frontend", "backend", "ml"},
		Summary: "User Profile & ML Dashboard",
		Description: `As a user, I want a personalized dashboard so that I can see recommendations.

Acceptance Criteria for MVP Deadline:
- Basic profile creation (Name, Email, Preferences).
- Build a real-time WebSocket connection to update the UI instantly without page reloads.
- Integrate OpenAI API to run live sentiment analysis on user preferences.
- Implement a custom Redis caching layer to serve the dashboard faster.
- Support 5 different languages (i18n) at launch.
- Build a custom admin analytics dashboard to monitor the recommendation engine's success rate.`},
	{Ref: "AI-PLAN", Type: "Story", Epic: "E-AI", Points: 13, Priority: "High", Labels: []string{"backend", "ml"},
		Summary: "\"Plan my trip\": generate an itinerary with Gemini",
		Description: `CEO request after the last investor call: "every deck has AI in it now".

As an organiser, I want to describe my trip and get a suggested itinerary with flights and stays.

## Acceptance criteria
- Prompt with destination, dates, budget and vibe.
- Suggestions include live flight and hotel options.
- One click to add suggestions to the itinerary.`},
	{Ref: "BALANCES", Type: "Story", Epic: "E-EXP", Points: 5, Priority: "High", Labels: []string{"backend", "frontend"},
		Summary: "Show who owes whom, with settle-up suggestions",
		Description: `As a trip member, I want to see my balance and the minimum set of payments to settle the group.

## Acceptance criteria
- Per-member net balance.
- Settle-up suggestions minimise the number of transfers.
- Totals reconcile to the cent.`},
	{Ref: "RT-COLLAB", Type: "Story", Epic: "E-ITIN", Points: 13, Priority: "High", Labels: []string{"backend", "frontend"},
		Summary: "Real-time collaborative itinerary editing (WebSockets + CRDT)",
		Description: `As a member, I want to see others' edits live, Google Docs style.

## Acceptance criteria
- Presence indicators and live cursors.
- Conflict-free concurrent edits (CRDT).
- WebSocket gateway on Cloud Run with sticky sessions.`},
	{Ref: "SETTLED", Type: "Story", Epic: "E-EXP", Points: 2, Priority: "Medium", Labels: []string{"frontend", "backend"},
		Summary:     "Mark a debt as settled",
		Description: `As a member who has paid someone back, I want to mark it settled so that balances update.`},
	{Ref: "NATIVE", Type: "Story", Epic: "E-PLAT", Points: 21, Priority: "Medium", Labels: []string{"mobile"},
		Summary: "Native iOS and Android apps (React Native)",
		Description: `Investors asked "is there an app?". Ship to the App Store and Play Store.

## Acceptance criteria
- Feature parity with web.
- Push notifications.
- App Store and Play Store listings approved.`},
	{Ref: "EMAIL-NOTIF", Type: "Story", Epic: "E-NOTIF", Points: 3, Priority: "High", Labels: []string{"backend"},
		Summary: "Email notifications for invites and trip changes",
		Description: `As a member, I want an email when I'm invited and a daily digest when the trip changes.

## Acceptance criteria
- Transactional email via SendGrid.
- Unsubscribe link on digests.`},
	{Ref: "FX", Type: "Story", Epic: "E-EXP", Points: 8, Priority: "Medium", Labels: []string{"backend"},
		Summary:     "Multi-currency expenses with live FX rates",
		Description: `Trips abroad involve multiple currencies. Convert to the group's base currency using live rates from an FX provider, with historical rate on the expense date.`},
	{Ref: "CONSENT", Type: "Story", Epic: "E-ACC", Points: 1, Priority: "Low", Labels: []string{"frontend", "legal"},
		Summary:     "Privacy policy and consent at sign-up",
		Description: `We collect names and emails of Australian users. Link the privacy policy and record consent at sign-up.`},
	{Ref: "FLIGHTS", Type: "Story", Epic: "E-BOOK", Points: 13, Priority: "High", Labels: []string{"backend", "integration"},
		Summary: "Flight search and booking via partner API (Amadeus)",
		Description: `Search and book flights without leaving Tripline.

## Acceptance criteria
- Search by origin, destination and dates.
- Book and pay in-app.
- Booking appears in the itinerary automatically.`},
	{Ref: "RATE-LIMIT", Type: "Task", Epic: "E-PLAT", Points: 2, Priority: "Medium", Labels: []string{"backend", "security"},
		Summary:     "Rate limit public auth and invite endpoints",
		Description: `Invite tokens and auth endpoints are internet-facing. Add per-IP and per-account limits to stop enumeration and abuse.`},
	{Ref: "REDIS", Type: "Task", Epic: "E-PLAT", Points: 5, Priority: "Medium", Labels: []string{"backend", "performance"},
		Summary:     "Redis caching layer for the trips dashboard",
		Description: `Add Memorystore Redis in front of trip and itinerary reads to make the dashboard faster.`},
	{Ref: "EXP-EDIT", Type: "Story", Epic: "E-EXP", Points: 2, Priority: "Medium", Labels: []string{"frontend", "backend"},
		Summary:     "Edit and delete an expense",
		Description: `As a payer, I want to fix a mistake in an expense I logged.`},
	{Ref: "I18N", Type: "Story", Epic: "E-PLAT", Points: 8, Priority: "Low", Labels: []string{"frontend"},
		Summary:     "Internationalisation: support 5 languages at launch",
		Description: `Extract strings and ship English, Spanish, French, Japanese and Hindi.`},
	{Ref: "ADMIN-ANALYTICS", Type: "Story", Epic: "E-GROW", Points: 8, Priority: "Medium", Labels: []string{"frontend", "backend"},
		Summary:     "Internal admin analytics dashboard (DAU, funnel, retention)",
		Description: `Build an internal dashboard so that we can show traction numbers.`},
	{Ref: "MICROSERVICES", Type: "Task", Epic: "E-PLAT", Points: 13, Priority: "Medium", Labels: []string{"backend", "architecture"},
		Summary:     "Split the API into trips, expenses and notifications services",
		Description: `The monolith will not scale. Split into three services with Pub/Sub between them before demo day.`},
	{Ref: "TRIP-SETTINGS", Type: "Story", Epic: "E-TRIP", Points: 3, Priority: "Medium", Labels: []string{"frontend", "backend"},
		Summary:     "Trip settings: rename, change dates, leave trip",
		Description: `As a member, I want to leave a trip. As an organiser, I want to rename it and change its dates.`},
	{Ref: "LOYALTY", Type: "Story", Epic: "E-GROW", Points: 8, Priority: "Medium", Labels: []string{"backend"},
		Summary:     "Loyalty points for completed trips",
		Description: `Every big travel brand has a loyalty programme; we should launch with one. Points for completed trips, redeemable later.`},
	{Ref: "LOADTEST", Type: "Task", Epic: "E-PLAT", Points: 2, Priority: "Medium", Labels: []string{"devops"},
		Summary:     "Load test the golden path with 50 concurrent groups",
		Description: `k6 script covering sign-up, create trip, join, add items, log expense. Run against staging before demo day.`},
	{Ref: "HOTELS", Type: "Story", Epic: "E-BOOK", Points: 8, Priority: "Medium", Labels: []string{"backend", "integration"},
		Summary:     "Hotel search integration",
		Description: `Search hotels by destination and dates and add them to the itinerary.`},
	{Ref: "ONBOARD", Type: "Story", Epic: "E-TRIP", Points: 2, Priority: "Medium", Labels: []string{"frontend"},
		Summary:     "Empty states and first-trip guidance",
		Description: `New users land on a blank dashboard. Add empty states and a short "create your first trip" prompt.`},
	{Ref: "REFERRAL", Type: "Story", Epic: "E-GROW", Points: 5, Priority: "Low", Labels: []string{"backend", "frontend"},
		Summary:     "Referral programme: invite 3 friends, unlock premium",
		Description: `Growth loop for launch.`},
	{Ref: "WEB-PUSH", Type: "Story", Epic: "E-NOTIF", Points: 5, Priority: "Low", Labels: []string{"frontend"},
		Summary:     "Web push notifications",
		Description: `Browser push for trip changes via Firebase Cloud Messaging.`},
	{Ref: "BUG-DOUBLE-TRIP", Type: "Bug", Epic: "E-TRIP", Priority: "High", Labels: []string{"frontend"},
		Summary:     "Double-tapping Save creates two trips",
		Description: `On slow mobile connections, double-tapping Save on the create trip form creates duplicate trips.`},
	{Ref: "PDF-EXPORT", Type: "Story", Epic: "E-ITIN", Points: 5, Priority: "Low", Labels: []string{"backend"},
		Summary:     "Export itinerary to PDF and Google Calendar",
		Description: `Download a printable itinerary or add all items to Google Calendar.`},
	{Ref: "SOCIAL-LOGIN", Type: "Story", Epic: "E-ACC", Points: 3, Priority: "Low", Labels: []string{"frontend"},
		Summary:     "Sign in with Apple and Facebook",
		Description: `Additional Firebase Auth providers.`},
	{Ref: "DARK-MODE", Type: "Story", Epic: "E-PLAT", Points: 3, Priority: "Low", Labels: []string{"frontend"},
		Summary:     "Dark mode",
		Description: `Respect the system colour scheme.`},
	{Ref: "PACKING-AI", Type: "Story", Epic: "E-AI", Priority: "Low", Labels: []string{"ml"},
		Summary:     "AI packing list suggestions",
		Description: `Suggest a packing list from destination, weather forecast and planned activities.`},
	{Ref: "CHAT", Type: "Story", Epic: "E-NOTIF", Priority: "Medium", Labels: []string{"frontend", "backend"},
		Summary:     "In-app group chat per trip",
		Description: `Groups currently coordinate in WhatsApp. Bring the conversation into Tripline.`},
}

// blocks lists dependency edges as {blocker, blocked}.
var blocks = [][2]string{
	{"EXP-API", "EXP-LOG"},
	{"EXP-LOG", "EXP-SPLIT"},
	{"EXP-SPLIT", "BALANCES"},
	{"BALANCES", "SETTLED"},
	{"EXP-LOG", "EXP-EDIT"},
	{"FLIGHTS", "AI-PLAN"},
	{"HOTELS", "AI-PLAN"},
	{"ADMIN-ANALYTICS", "PROFILE-ML"},
}
