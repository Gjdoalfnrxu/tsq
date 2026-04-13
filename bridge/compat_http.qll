/**
 * CodeQL-compatible HTTP abstraction layer stubs.
 * Clean-room implementation providing request handler, server request,
 * and response body classes for framework-agnostic HTTP analysis.
 *
 * This file is NOT derived from CodeQL source code.
 * API surface documented from public CodeQL documentation.
 */

module HTTP {
    /**
     * An abstract HTTP request handler function.
     * Matches functions detected as handlers by any supported framework
     * (Express, Koa, Fastify, Next.js, Lambda, or generic HTTP).
     */
    abstract class RequestHandler extends @symbol {
        RequestHandler() {
            ExpressHandler(this) or
            HttpHandler(this) or
            KoaHandler(this) or
            FastifyHandler(this) or
            NextjsHandler(this) or
            LambdaHandler(this)
        }
    }

    /**
     * A server request parameter — the first parameter of an HTTP handler function.
     * Represents the incoming request object (e.g., req in Express, ctx in Koa).
     */
    class ServerRequest extends @symbol {
        ServerRequest() {
            exists(int fn |
                Parameter(fn, 0, _, _, this, _) and
                (ExpressHandler(fn) or HttpHandler(fn) or KoaHandler(fn) or FastifyHandler(fn) or NextjsHandler(fn) or LambdaHandler(fn))
            )
        }

        /** Gets a textual representation. */
        string toString() { Symbol(this, result, _, _) }
    }

    /**
     * A response body that is a taint sink for XSS.
     * Matches any TaintSink marked with the "xss" category.
     */
    class ResponseBody extends @taint_sink {
        ResponseBody() { TaintSink(this, "xss") }

        /** Gets a textual representation. */
        string toString() { result = "response body" }
    }
}
