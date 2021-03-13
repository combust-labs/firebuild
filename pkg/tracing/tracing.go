package tracing

import (
	"github.com/combust-labs/firebuild/configs"
	"github.com/hashicorp/go-hclog"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/uber/jaeger-client-go"
)

// GetTracer returns configured Jaeger reporter or null reporter, if tracer is disabled.
func GetTracer(logger hclog.Logger, config *configs.TracingConfig) (opentracing.Tracer, func(), error) {
	if config.Enable {
		transport, err := jaeger.NewUDPTransport(config.HostPort, 0)
		if err != nil {
			return nil, func() {}, errors.Wrap(err, "failed constructing jaeger UDP transport")
		}
		logAdapter := &adapter{log: logger}

		reporters := []jaeger.Reporter{}
		remoteReporterOptions := []jaeger.ReporterOption{}

		if config.LogEnable {
			reporters = append(reporters, jaeger.NewLoggingReporter(logAdapter))
			remoteReporterOptions = append(remoteReporterOptions, jaeger.ReporterOptions.Logger(logAdapter))
		}

		reporters = append(reporters, jaeger.NewRemoteReporter(transport, remoteReporterOptions...))

		reporter := jaeger.NewCompositeReporter(reporters...)
		tracer, closer := jaeger.NewTracer(config.ApplicationName,
			jaeger.NewConstSampler(true),
			reporter,
		)
		return tracer, func() {
			reporter.Close()
			closer.Close()
		}, nil
	}

	reporter := jaeger.NewNullReporter()
	tracer, closer := jaeger.NewTracer(config.ApplicationName,
		jaeger.NewConstSampler(true),
		reporter,
	)
	return tracer, func() {
		reporter.Close()
		closer.Close()
	}, nil
}
