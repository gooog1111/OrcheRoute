package online.gooog1111.orcheroute;

public final class FreeTurnFailurePolicyTest {
    public static void main(String[] args) {
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("connected", 1, true, false), "connected state is not a failure");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("captcha", 0, true, false), "captcha state is not a failure");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 1, true, false), "a surviving stream must not tear down VPN");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 0, false, false), "startup error is handled by startup poll");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 0, true, true), "stopping must not report another error");
        require(FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 0, true, false), "terminal failure must be reported");
        for (String state : new String[]{"connecting", "connected", "captcha", "error", "idle"}) {
            require(FreeTurnFailurePolicy.shouldReportUnavailable(state, 0, true, false), "zero-stream outage must be visible: " + state);
            require(!FreeTurnFailurePolicy.shouldReportUnavailable(state, 1, true, false), "surviving stream must remain usable: " + state);
            require(!FreeTurnFailurePolicy.shouldReportUnavailable(state, 0, false, false), "startup has separate state: " + state);
            require(!FreeTurnFailurePolicy.shouldReportUnavailable(state, 0, true, true), "stop is not another outage: " + state);
        }
    }

    private static void require(boolean value, String scenario) {
        if (!value) throw new AssertionError(scenario);
    }
}
