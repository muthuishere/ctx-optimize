namespace App;

public class Engine
{
    public int Add(int a, int b) { return Scale(a) + b; }

    private int Scale(int a) { return a * 2; }
}
