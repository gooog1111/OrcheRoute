package online.gooog1111.orcheroute;

public final class FreeTurnFailurePolicyTest {
    public static void main(String[] args) {
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("connected", 1, true, false), "connected state is not a failure");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("captcha", 0, true, false), "captcha state is not a failure");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 1, true, false), "a surviving stream must not tear down VPN");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 0, false, false), "startup error is handled by startup poll");
        require(!FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 0, true, true), "stopping must not report another error");
        require(FreeTurnFailurePolicy.shouldReportTerminalFailure("error", 0, true, false), "terminal failure must be reported");
    }

    private static void require(boolean value, String scenario) {
        if (!value) throw new AssertionError(scenario);
    }
}
