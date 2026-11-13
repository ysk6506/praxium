// Package access is the unit that decides who may do what: the subject, the
// eight roles, the role table, and the two ports the outside plugs into. The
// Authenticator turns a bearer token into a subject; the Authorizer answers
// whether a subject may do an ability on a target and, when not, with which
// closed reason code (access-control.md, rest-api.md section 4).
//
// The first Authorizer here is the role table of the design, with every
// subject's scope taken as whole. Scoped grants, object conditions and the
// OIDC authenticator come with the identity backend; this unit reads the
// kernel and nothing else.
package access
