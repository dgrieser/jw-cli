package i18n

// AuthText is the text of the accounts of jw serve: the sign-in, registration
// and password pages, the admin pages, and the mails the server sends. Fields
// holding a "%" are fmt format strings; their arguments are named in the
// comment.
type AuthText struct {
	SignIn, SignOut, Register, Forgot, ResetTitle   string
	Email, Password, PasswordRepeat                 string
	PasswordNew, PasswordOld, ChangePassword        string
	PasswordHint                                    string // %[1]d the minimum length
	NoAccount, BackToSignIn, SendLink, Save         string
	RegisterIntro, RegisterIntroApproval            string
	RegisterSent, ForgotIntro, ForgotSent           string
	SetPasswordRegister                             string // %[1]s the address
	SetPasswordInvite, SetPasswordReset             string // %[1]s the address
	PendingNotice, PasswordChanged, PasswordsDiffer string
	SignedInAs                                      string // %[1]s the address

	Account, Role, RoleAdmin, RoleUser, Users, ServerSettings string
	Status, StatusActive, StatusPending, StatusDisabled       string
	StatusInvited, Created, LastLogin, Never, Actions, You    string
	Approve, Disable, Enable, MakeAdmin, MakeUser             string
	SendReset, ResendInvite, Delete                           string
	ConfirmDelete                                             string // %[1]s the address
	CreateUser, CreateUserHint, CreateUserNoMail              string
	PasswordOptional, Add, PendingCount                       string // PendingCount: %[1]d accounts

	MsgCreated, MsgInvited, MsgApproved, MsgDisabled, MsgEnabled string // %[1]s the address
	MsgRole, MsgResetSent, MsgDeleted                            string // %[1]s the address
	MsgSaved                                                     string
	MsgTestSent                                                  string // %[1]s the address

	SecAccess, Registration, RegClosed, RegApproval, RegOpen string
	Anonymous, AnonymousHint, PublicURL, PublicURLHint       string
	SecMail, MailHint, SMTPHost, SMTPPort, SMTPSecurity      string
	SecStartTLS, SecTLS, SecNone                             string
	SMTPUser, SMTPPassword, SMTPPasswordKeep                 string
	SMTPFrom, SMTPFromHint, SendTest, TestTo                 string

	SetupTitle, SetupIntro, SetupNeedLink, SecAdmin, SetupSave, SetupAdminHint string

	ErrBadLogin, ErrPending, ErrDisabled, ErrExists, ErrNotFound string
	ErrLastAdmin, ErrBadEmail, ErrNoPass, ErrBadToken            string
	ErrBadPass                                                   string // %[1]d the minimum length
	ErrTooMany                                                   string // %[1]d minutes
	ErrBadPublicURL, ErrRegisterNoMail, ErrBadSMTP               string
	ErrMail                                                      string // %[1]s the failure
	ErrSelf, ErrNoMail, ErrRegisterClosed                        string

	// Mails: %[1]s names the site (its address); %[2]s is the link.
	MailRegisterSubject, MailRegisterBody string
	MailExistsSubject, MailExistsBody     string
	MailResetSubject, MailResetBody       string
	MailInviteSubject, MailInviteBody     string
	MailApprovedSubject, MailApprovedBody string
	// MailPendingBody: %[1]s the site, %[2]s the link, %[3]s the new address.
	MailPendingSubject, MailPendingBody string
	MailTestSubject                     string
	MailTestBody                        string // %[1]s the site
}

var authEN = AuthText{
	SignIn: "Sign in", SignOut: "Sign out", Register: "Create account", Forgot: "Forgot your password?",
	ResetTitle: "Reset password", Email: "Email address", Password: "Password", PasswordRepeat: "Repeat password",
	PasswordNew: "New password", PasswordOld: "Current password", ChangePassword: "Change password",
	PasswordHint: "At least %[1]d characters.",
	NoAccount:    "No account yet?", BackToSignIn: "Back to sign in", SendLink: "Send link", Save: "Save",
	RegisterIntro:         "Enter your email address. You get a link to confirm it and choose a password.",
	RegisterIntroApproval: "Enter your email address. You get a link to confirm it and choose a password. An admin approves the account before you can sign in.",
	RegisterSent:          "Check your inbox: a link to continue is on its way. It is valid for 24 hours. Nothing there? Look in the spam folder.",
	ForgotIntro:           "Enter the email address of your account. You get a link to choose a new password.",
	ForgotSent:            "If an account uses this address, a link to choose a new password is on its way. It is valid for one hour.",
	SetPasswordRegister:   "Choose a password to finish the registration of %[1]s.",
	SetPasswordInvite:     "You were invited. Choose a password for %[1]s.",
	SetPasswordReset:      "Choose a new password for %[1]s.",
	PendingNotice:         "Your account is waiting for an admin's approval. You get an email once you can sign in.",
	PasswordChanged:       "Your password was changed. Your other sessions are signed out.",
	PasswordsDiffer:       "The passwords differ.",
	SignedInAs:            "Signed in as %[1]s",

	Account: "Account", Role: "Role", RoleAdmin: "Admin", RoleUser: "User", Users: "Users", ServerSettings: "Server settings",
	Status: "Status", StatusActive: "active", StatusPending: "waiting for approval", StatusDisabled: "disabled",
	StatusInvited: "invited", Created: "Created", LastLogin: "Last sign-in", Never: "never", Actions: "Actions", You: "you",
	Approve: "Approve", Disable: "Disable", Enable: "Enable", MakeAdmin: "Make admin", MakeUser: "Make user",
	SendReset: "Send password link", ResendInvite: "Resend invitation", Delete: "Delete",
	ConfirmDelete:    "Delete the account %[1]s?",
	CreateUser:       "Add account",
	CreateUserHint:   "Without a password, an invitation goes out by email to choose one.",
	CreateUserNoMail: "No mail server is set up: give the account a password and tell it to its owner.",
	PasswordOptional: "Password (optional)", Add: "Add", PendingCount: "%[1]d waiting",

	MsgCreated: "Account %[1]s added.", MsgInvited: "Invitation sent to %[1]s.", MsgApproved: "%[1]s approved.",
	MsgDisabled: "%[1]s disabled.", MsgEnabled: "%[1]s enabled.", MsgRole: "Role of %[1]s changed.",
	MsgResetSent: "Password link sent to %[1]s.", MsgDeleted: "%[1]s deleted.", MsgSaved: "Settings saved.",
	MsgTestSent: "Test email sent to %[1]s.",

	SecAccess: "Access", Registration: "Registration",
	RegClosed:     "Closed: only admins add accounts",
	RegApproval:   "Open, with approval: anybody registers, an admin lets them in",
	RegOpen:       "Open: anybody with a confirmed email address",
	Anonymous:     "Reading without an account",
	AnonymousHint: "Anybody who reaches the server reads without signing in; accounts are for the admins.",
	PublicURL:     "Public address",
	PublicURLHint: "The address readers open, e.g. https://jw.example.org. The links in emails lead there.",
	SecMail:       "Email",
	MailHint:      "Needed for registration, invitations and forgotten passwords. Without it, admins set the passwords.",
	SMTPHost:      "Mail server (SMTP)", SMTPPort: "Port", SMTPSecurity: "Encryption",
	SecStartTLS: "STARTTLS (usually port 587)", SecTLS: "TLS (usually port 465)", SecNone: "None (a relay on this machine only)",
	SMTPUser: "User name", SMTPPassword: "Password", SMTPPasswordKeep: "Leave empty to keep the stored password.",
	SMTPFrom: "Sender", SMTPFromHint: "e.g. JW <jw@example.org>", SendTest: "Send test email", TestTo: "Test email to",

	SetupTitle:     "Set up jw serve",
	SetupIntro:     "Choose who may use this server and how it sends email, and create the first admin account.",
	SetupNeedLink:  "Open the setup link jw serve printed when it started (…/setup?token=…).",
	SecAdmin:       "Admin account",
	SetupSave:      "Save and sign in",
	SetupAdminHint: "The email address is the user name. An existing account becomes the admin with its password.",

	ErrBadLogin: "Wrong email address or password.", ErrPending: "The account is waiting for an admin's approval.",
	ErrDisabled: "The account is disabled.", ErrExists: "An account with this email address exists already.",
	ErrNotFound: "No such account.", ErrLastAdmin: "The last admin cannot be removed, disabled or demoted.",
	ErrBadEmail: "That is not a valid email address.", ErrNoPass: "The account has no password yet: use the link from the invitation.",
	ErrBadToken: "The link is invalid or has expired.", ErrBadPass: "The password needs at least %[1]d characters.",
	ErrTooMany:        "Too many attempts. Try again in %[1]d minutes.",
	ErrBadPublicURL:   "The public address must be an http:// or https:// address without a path.",
	ErrRegisterNoMail: "Registration needs a mail server and the public address: the confirmation goes out by email.",
	ErrBadSMTP:        "The mail server needs a host, a port and a sender address.",
	ErrMail:           "The email could not be sent: %[1]s",
	ErrSelf:           "Not for your own account.", ErrNoMail: "No mail server is set up.", ErrRegisterClosed: "Registration is closed.",

	MailRegisterSubject: "Confirm your email address",
	MailRegisterBody: "Hello,\n\nsomebody, hopefully you, wants to create an account on %[1]s with this email address.\n" +
		"To confirm it and choose a password, open this link (valid for 24 hours):\n\n%[2]s\n\n" +
		"If it was not you, ignore this email; no account is created.\n",
	MailExistsSubject: "Your account",
	MailExistsBody: "Hello,\n\nsomebody, hopefully you, wanted to create an account on %[1]s with this email address, " +
		"but it has one already.\nForgot the password? Choose a new one with this link (valid for one hour):\n\n%[2]s\n\n" +
		"If it was not you, ignore this email.\n",
	MailResetSubject: "Choose a new password",
	MailResetBody: "Hello,\n\nto choose a new password for your account on %[1]s, open this link (valid for one hour):\n\n%[2]s\n\n" +
		"If you did not ask for it, ignore this email; your password stays as it is.\n",
	MailInviteSubject: "Your invitation",
	MailInviteBody: "Hello,\n\nan admin made an account for you on %[1]s.\n" +
		"To choose your password, open this link (valid for 7 days):\n\n%[2]s\n",
	MailApprovedSubject: "Your account is approved",
	MailApprovedBody:    "Hello,\n\nyour account on %[1]s is approved. Sign in here:\n\n%[2]s\n",
	MailPendingSubject:  "A new account waits for approval",
	MailPendingBody:     "Hello,\n\n%[3]s registered on %[1]s and waits for approval:\n\n%[2]s\n",
	MailTestSubject:     "Test email",
	MailTestBody:        "Hello,\n\nthis is a test email from %[1]s. The mail settings work.\n",
}

var authDE = AuthText{
	SignIn: "Anmelden", SignOut: "Abmelden", Register: "Konto anlegen", Forgot: "Passwort vergessen?",
	ResetTitle: "Passwort zurücksetzen", Email: "E-Mail-Adresse", Password: "Passwort", PasswordRepeat: "Passwort wiederholen",
	PasswordNew: "Neues Passwort", PasswordOld: "Bisheriges Passwort", ChangePassword: "Passwort ändern",
	PasswordHint: "Mindestens %[1]d Zeichen.",
	NoAccount:    "Noch kein Konto?", BackToSignIn: "Zurück zur Anmeldung", SendLink: "Link senden", Save: "Speichern",
	RegisterIntro:         "Gib deine E-Mail-Adresse ein. Du bekommst einen Link, um sie zu bestätigen und ein Passwort zu wählen.",
	RegisterIntroApproval: "Gib deine E-Mail-Adresse ein. Du bekommst einen Link, um sie zu bestätigen und ein Passwort zu wählen. Ein Admin gibt das Konto frei, bevor du dich anmelden kannst.",
	RegisterSent:          "Schau in dein Postfach: Ein Link zum Weitermachen ist unterwegs. Er gilt 24 Stunden. Nichts da? Schau im Spam-Ordner nach.",
	ForgotIntro:           "Gib die E-Mail-Adresse deines Kontos ein. Du bekommst einen Link, um ein neues Passwort zu wählen.",
	ForgotSent:            "Wenn ein Konto diese Adresse hat, ist ein Link für ein neues Passwort unterwegs. Er gilt eine Stunde.",
	SetPasswordRegister:   "Wähle ein Passwort, um die Registrierung von %[1]s abzuschließen.",
	SetPasswordInvite:     "Du wurdest eingeladen. Wähle ein Passwort für %[1]s.",
	SetPasswordReset:      "Wähle ein neues Passwort für %[1]s.",
	PendingNotice:         "Dein Konto wartet auf die Freigabe durch einen Admin. Du bekommst eine E-Mail, sobald du dich anmelden kannst.",
	PasswordChanged:       "Dein Passwort wurde geändert. Deine anderen Sitzungen sind abgemeldet.",
	PasswordsDiffer:       "Die Passwörter sind verschieden.",
	SignedInAs:            "Angemeldet als %[1]s",

	Account: "Konto", Role: "Rolle", RoleAdmin: "Admin", RoleUser: "Benutzer", Users: "Benutzer", ServerSettings: "Server-Einstellungen",
	Status: "Status", StatusActive: "aktiv", StatusPending: "wartet auf Freigabe", StatusDisabled: "gesperrt",
	StatusInvited: "eingeladen", Created: "Angelegt", LastLogin: "Letzte Anmeldung", Never: "nie", Actions: "Aktionen", You: "du",
	Approve: "Freigeben", Disable: "Sperren", Enable: "Entsperren", MakeAdmin: "Zum Admin machen", MakeUser: "Zum Benutzer machen",
	SendReset: "Passwort-Link senden", ResendInvite: "Einladung erneut senden", Delete: "Löschen",
	ConfirmDelete:    "Das Konto %[1]s löschen?",
	CreateUser:       "Konto hinzufügen",
	CreateUserHint:   "Ohne Passwort geht eine Einladung per E-Mail hinaus, mit der eines gewählt wird.",
	CreateUserNoMail: "Kein Mailserver eingerichtet: Gib dem Konto ein Passwort und sag es seinem Besitzer.",
	PasswordOptional: "Passwort (optional)", Add: "Hinzufügen", PendingCount: "%[1]d warten",

	MsgCreated: "Konto %[1]s hinzugefügt.", MsgInvited: "Einladung an %[1]s gesendet.", MsgApproved: "%[1]s freigegeben.",
	MsgDisabled: "%[1]s gesperrt.", MsgEnabled: "%[1]s entsperrt.", MsgRole: "Rolle von %[1]s geändert.",
	MsgResetSent: "Passwort-Link an %[1]s gesendet.", MsgDeleted: "%[1]s gelöscht.", MsgSaved: "Einstellungen gespeichert.",
	MsgTestSent: "Test-E-Mail an %[1]s gesendet.",

	SecAccess: "Zugang", Registration: "Registrierung",
	RegClosed:     "Geschlossen: nur Admins legen Konten an",
	RegApproval:   "Offen mit Freigabe: jeder registriert sich, ein Admin lässt ihn herein",
	RegOpen:       "Offen: jeder mit bestätigter E-Mail-Adresse",
	Anonymous:     "Lesen ohne Konto",
	AnonymousHint: "Jeder, der den Server erreicht, liest ohne Anmeldung; Konten sind für die Admins.",
	PublicURL:     "Öffentliche Adresse",
	PublicURLHint: "Die Adresse, die Leser öffnen, z. B. https://jw.example.org. Die Links in E-Mails führen dorthin.",
	SecMail:       "E-Mail",
	MailHint:      "Nötig für Registrierung, Einladungen und vergessene Passwörter. Ohne setzen Admins die Passwörter.",
	SMTPHost:      "Mailserver (SMTP)", SMTPPort: "Port", SMTPSecurity: "Verschlüsselung",
	SecStartTLS: "STARTTLS (meist Port 587)", SecTLS: "TLS (meist Port 465)", SecNone: "Keine (nur ein Relay auf diesem Rechner)",
	SMTPUser: "Benutzername", SMTPPassword: "Passwort", SMTPPasswordKeep: "Leer lassen, um das gespeicherte Passwort zu behalten.",
	SMTPFrom: "Absender", SMTPFromHint: "z. B. JW <jw@example.org>", SendTest: "Test-E-Mail senden", TestTo: "Test-E-Mail an",

	SetupTitle:     "jw serve einrichten",
	SetupIntro:     "Lege fest, wer diesen Server nutzen darf und wie er E-Mails sendet, und lege das erste Admin-Konto an.",
	SetupNeedLink:  "Öffne den Einrichtungslink, den jw serve beim Start ausgegeben hat (…/setup?token=…).",
	SecAdmin:       "Admin-Konto",
	SetupSave:      "Speichern und anmelden",
	SetupAdminHint: "Die E-Mail-Adresse ist der Benutzername. Ein vorhandenes Konto wird mit seinem Passwort zum Admin.",

	ErrBadLogin: "E-Mail-Adresse oder Passwort falsch.", ErrPending: "Das Konto wartet auf die Freigabe durch einen Admin.",
	ErrDisabled: "Das Konto ist gesperrt.", ErrExists: "Ein Konto mit dieser E-Mail-Adresse gibt es schon.",
	ErrNotFound: "Konto nicht gefunden.", ErrLastAdmin: "Der letzte Admin kann nicht entfernt, gesperrt oder zurückgestuft werden.",
	ErrBadEmail: "Das ist keine gültige E-Mail-Adresse.", ErrNoPass: "Das Konto hat noch kein Passwort: Nutze den Link aus der Einladung.",
	ErrBadToken: "Der Link ist ungültig oder abgelaufen.", ErrBadPass: "Das Passwort braucht mindestens %[1]d Zeichen.",
	ErrTooMany:        "Zu viele Versuche. Bitte in %[1]d Minuten noch einmal.",
	ErrBadPublicURL:   "Die öffentliche Adresse muss eine http://- oder https://-Adresse ohne Pfad sein.",
	ErrRegisterNoMail: "Die Registrierung braucht einen Mailserver und die öffentliche Adresse: Die Bestätigung geht per E-Mail hinaus.",
	ErrBadSMTP:        "Der Mailserver braucht einen Host, einen Port und eine Absenderadresse.",
	ErrMail:           "Die E-Mail konnte nicht gesendet werden: %[1]s",
	ErrSelf:           "Nicht für dein eigenes Konto.", ErrNoMail: "Kein Mailserver eingerichtet.", ErrRegisterClosed: "Die Registrierung ist geschlossen.",

	MailRegisterSubject: "Bestätige deine E-Mail-Adresse",
	MailRegisterBody: "Hallo,\n\njemand, hoffentlich du, möchte auf %[1]s ein Konto mit dieser E-Mail-Adresse anlegen.\n" +
		"Um sie zu bestätigen und ein Passwort zu wählen, öffne diesen Link (gültig 24 Stunden):\n\n%[2]s\n\n" +
		"Warst du es nicht, ignoriere diese E-Mail; es wird kein Konto angelegt.\n",
	MailExistsSubject: "Dein Konto",
	MailExistsBody: "Hallo,\n\njemand, hoffentlich du, wollte auf %[1]s ein Konto mit dieser E-Mail-Adresse anlegen, " +
		"es gibt aber schon eins.\nPasswort vergessen? Wähle mit diesem Link ein neues (gültig eine Stunde):\n\n%[2]s\n\n" +
		"Warst du es nicht, ignoriere diese E-Mail.\n",
	MailResetSubject: "Wähle ein neues Passwort",
	MailResetBody: "Hallo,\n\num ein neues Passwort für dein Konto auf %[1]s zu wählen, öffne diesen Link (gültig eine Stunde):\n\n%[2]s\n\n" +
		"Hast du nicht danach gefragt, ignoriere diese E-Mail; dein Passwort bleibt, wie es ist.\n",
	MailInviteSubject: "Deine Einladung",
	MailInviteBody: "Hallo,\n\nein Admin hat dir auf %[1]s ein Konto angelegt.\n" +
		"Um dein Passwort zu wählen, öffne diesen Link (gültig 7 Tage):\n\n%[2]s\n",
	MailApprovedSubject: "Dein Konto ist freigegeben",
	MailApprovedBody:    "Hallo,\n\ndein Konto auf %[1]s ist freigegeben. Hier anmelden:\n\n%[2]s\n",
	MailPendingSubject:  "Ein neues Konto wartet auf Freigabe",
	MailPendingBody:     "Hallo,\n\n%[3]s hat sich auf %[1]s registriert und wartet auf Freigabe:\n\n%[2]s\n",
	MailTestSubject:     "Test-E-Mail",
	MailTestBody:        "Hallo,\n\ndies ist eine Test-E-Mail von %[1]s. Die E-Mail-Einstellungen funktionieren.\n",
}

var authCatalog = map[Lang]*AuthText{EN: &authEN, DE: &authDE}

// Auth returns the accounts text for the language.
func (l Lang) Auth() *AuthText {
	if m, ok := authCatalog[l]; ok {
		return m
	}
	return authCatalog[EN]
}

// AuthFor returns the accounts text for a locale or BCP-47 tag.
func AuthFor(tag string) *AuthText { return Match(tag).Auth() }
