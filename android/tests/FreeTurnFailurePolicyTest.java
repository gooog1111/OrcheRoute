package online.gooog1111.orcheroute;

public final class FreeTurnFailurePolicyTest {
    public static void main(String[] args) {
        require(!FreeTurnFailurePolicy.shouldRecover("connected", true, false), "connected state is not a failure");
        require(!FreeTurnFailurePolicy.shouldRecover("captcha", true, false), "captcha state is not a failure");
        require(!FreeTurnFailurePolicy.shouldRecover("error", false, false), "error before connect is handled by the startup poll");
        require(!FreeTurnFailurePolicy.shouldRecover("error", true, true), "already stopping must not trigger another reload");
        require(FreeTurnFailurePolicy.shouldRecover("error", true, false), "post-connect error must trigger a reload");
    }

    private static void require(boolean value, String scenario) {
        if (!value) throw new AssertionError(scenario);
    }
}
