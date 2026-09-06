package online.gooog1111.orcheroute;

/**
 * Pure decision kept separate so the post-connect FreeTURN terminal-failure
 * reaction can be tested without Android. The native bridge reports "error"
 * both while still negotiating a connection (already handled by the
 * synchronous startup poll in OrcheRouteVpnService#startFreeTURN) and after a
 * connection was established and later dropped. Only the latter case must
 * trigger a reload — reacting to a startup-phase error here would race the
 * poll loop that already throws and reports it.
 */
final class FreeTurnFailurePolicy {
    private FreeTurnFailurePolicy() { }

    static boolean shouldRecover(String state, boolean connected, boolean stopping) {
        return "error".equals(state) && connected && !stopping;
    }
}
