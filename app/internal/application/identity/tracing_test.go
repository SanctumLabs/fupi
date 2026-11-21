package identityapp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sanctumlabs/fupi/app/internal/core/entity"
	"github.com/sanctumlabs/fupi/app/internal/domain/identity"
	"github.com/sanctumlabs/fupi/app/pkg/errdefs"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"
)

// traced makes the service record its spans into the returned exporter, under a "request" span like the HTTP middleware's.
func (suite *IdentityServiceTestSuite) traced() (context.Context, *tracetest.InMemoryExporter, trace.Span) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	suite.T().Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	suite.service.tracer = provider.Tracer("test")

	ctx, request := provider.Tracer("test").Start(context.Background(), "request")
	return ctx, exporter, request
}

// useCaseSpan returns the one span a use case recorded; the request span is still open, so it is not in the exporter.
func (suite *IdentityServiceTestSuite) useCaseSpan(exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	spans := exporter.GetSpans()
	suite.Require().Len(spans, 1)
	suite.Equal(name, spans[0].Name)
	return spans[0]
}

// spanText is everything a span could leak: its status text, attributes and events.
func spanText(span tracetest.SpanStub) string {
	text := span.Status.Description
	for _, attribute := range span.Attributes {
		text += " " + attribute.Value.String()
	}
	for _, event := range span.Events {
		text += " " + event.Name
		for _, attribute := range event.Attributes {
			text += " " + attribute.Value.String()
		}
	}
	return text
}

func (suite *IdentityServiceTestSuite) TestRegister_RecordsASpanBeneathTheRequestSpan() {
	ctx, exporter, request := suite.traced()
	cmd := validCommand()
	suite.mockUsers.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, user identity.User) (identity.User, error) { return user, nil })
	suite.mockNotifier.EXPECT().SendEmailVerification(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	_, err := suite.service.Register(ctx, cmd)
	suite.Require().NoError(err)

	span := suite.useCaseSpan(exporter, "identity.Register")
	suite.Equal(request.SpanContext().SpanID(), span.Parent.SpanID(), "the use case nests under the request")
	suite.Equal(codes.Unset, span.Status.Code)
	for _, secret := range []string{cmd.Email, cmd.Username, cmd.Password, cmd.FirstName} {
		suite.NotContains(spanText(span), secret, "a span must not carry what the caller sent")
	}
}

func (suite *IdentityServiceTestSuite) TestRegister_AFailureMarksTheSpanWithTheClassOfTheErrorNotItsText() {
	ctx, exporter, _ := suite.traced()
	cmd := validCommand()
	cmd.Email = "not-an-email"

	_, err := suite.service.Register(ctx, cmd)
	suite.Require().Error(err)
	suite.Require().Contains(err.Error(), "not-an-email", "the premise: this error's own text embeds what the caller sent")

	span := suite.useCaseSpan(exporter, "identity.Register")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("invalid_parameter", span.Status.Description)
	suite.NotContains(spanText(span), "not-an-email")
	for _, event := range span.Events {
		suite.NotEqual("exception", event.Name, "RecordError would copy the error text into the span")
	}
}

func (suite *IdentityServiceTestSuite) TestLogin_RecordsASpanAndMarksAWrongPasswordAsUnauthorized() {
	ctx, exporter, _ := suite.traced()
	user := suite.registeredUser("the-right-password")
	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil)

	_, _, err := suite.service.Login(ctx, "john.doe@fupi.com", "the-wrong-password")
	suite.Require().Error(err)

	span := suite.useCaseSpan(exporter, "identity.Login")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("unauthorized", span.Status.Description)
	suite.NotContains(spanText(span), "john.doe@fupi.com")
	suite.NotContains(spanText(span), "the-wrong-password")
}

func (suite *IdentityServiceTestSuite) TestLogin_ASuccessLeavesTheSpanUnset() {
	ctx, exporter, _ := suite.traced()
	const password = "s3cret-password"
	user := suite.registeredUser(password)
	userID := entity.IDToString(user.ID())
	suite.mockUsers.EXPECT().FetchByEmail(gomock.Any(), gomock.Any()).Return(*user, nil)
	suite.mockTokens.EXPECT().GenerateAccessToken(userID).Return("access-token", nil)
	suite.mockTokens.EXPECT().GenerateRefreshToken(userID).Return("refresh-token", nil)

	_, _, err := suite.service.Login(ctx, "john.doe@fupi.com", password)
	suite.Require().NoError(err)

	span := suite.useCaseSpan(exporter, "identity.Login")
	suite.Equal(codes.Unset, span.Status.Code)
	suite.NotContains(spanText(span), "access-token", "tokens are never recorded")
}

func (suite *IdentityServiceTestSuite) TestRefresh_RecordsASpanAndNeverTheToken() {
	ctx, exporter, _ := suite.traced()
	suite.mockTokens.EXPECT().Authenticate("a-bad-refresh-token").Return("", errdefs.Unauthorized(errors.New("bad signature")))

	_, err := suite.service.Refresh(ctx, "a-bad-refresh-token")
	suite.Require().Error(err)

	span := suite.useCaseSpan(exporter, "identity.Refresh")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("unauthorized", span.Status.Description)
	suite.NotContains(spanText(span), "a-bad-refresh-token")
}

func (suite *IdentityServiceTestSuite) TestVerifyEmail_RecordsASpanAndNeverTheToken() {
	ctx, exporter, _ := suite.traced()
	suite.mockUsers.EXPECT().FetchByVerificationToken(gomock.Any(), "a-secret-token").
		Return(identity.User{}, errdefs.NotFound(errors.New("no rows")))

	_, err := suite.service.VerifyEmail(ctx, "a-secret-token")
	suite.Require().Error(err)

	span := suite.useCaseSpan(exporter, "identity.VerifyEmail")
	suite.Equal(codes.Error, span.Status.Code)
	suite.Equal("invalid_parameter", span.Status.Description, "the use case reports an unknown token as an invalid one")
	suite.NotContains(spanText(span), "a-secret-token")
}

func TestErrorClass(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"invalid parameter": {errdefs.InvalidParameter(errors.New("x")), "invalid_parameter"},
		"unauthorized":      {errdefs.Unauthorized(errors.New("x")), "unauthorized"},
		"forbidden":         {errdefs.Forbidden(errors.New("x")), "forbidden"},
		"not found":         {errdefs.NotFound(errors.New("x")), "not_found"},
		"conflict":          {errdefs.Conflict(errors.New("x")), "conflict"},
		"unavailable":       {errdefs.Unavailable(errors.New("x")), "unavailable"},
		"wrapped":           {fmt.Errorf("context: %w", errdefs.Conflict(errors.New("x"))), "conflict"},
		"unclassified":      {errors.New("connection refused"), "internal"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, errorClass(tc.err))
		})
	}
}
