package online.gooog1111.orcheroute;

public final class BootPolicyTest {
    public static void main(String[] args) {
        require(!BootPolicy.shouldRestore(false, false), "disabled without permission");
        require(!BootPolicy.shouldRestore(false, true), "disabled with permission");
        require(!BootPolicy.shouldRestore(true, false), "enabled without permission");
        require(BootPolicy.shouldRestore(true, true), "enabled with permission");
    }

    private static void require(boolean value, String scenario) {
        if (!value) throw new AssertionError(scenario);
    }
}
