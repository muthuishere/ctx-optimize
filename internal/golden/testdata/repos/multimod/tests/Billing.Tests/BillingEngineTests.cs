namespace Golden.Billing.Tests;

public class BillingEngineTests
{
    public void ChargeCardWorks()
    {
        var engine = new BillingEngine();
        engine.ChargeCard();
    }
}
