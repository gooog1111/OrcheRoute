package online.gooog1111.orcheroute;

/**
 * Pure decision kept separate so the post-connect FreeTURN terminal-failure
 * reaction can be tested without Android. The native bridge reports "error"
 * both while still negotiating a connection (already handled by the
 * synchronous startup poll in OrcheRouteVpnService#startFreeTURN) and after a
 * connection was established and later dropped. Only the latter case must
 * be displayed to the user. A surviving stream must not tear down the VPN.
 */
final class FreeTurnFailurePolicy {
    private FreeTurnFailurePolicy() { }

    static boolean shouldReportTerminalFailure(String state, long streams, boolean connected, boolean stopping) {
        return "error".equals(state) && streams == 0 && connected && !stopping;
    }

    static boolean shouldReportUnavailable(String state, long streams, boolean connected, boolean stopping) {
        if (!connected || stopping || streams != 0) return false;
        return shouldReportTerminalFailure(state, streams, connected, stopping)
                || "connecting".equals(state) || "connected".equals(state)
                || "captcha".equals(state) || "idle".equals(state);
    }
}
