// Every shape of store the interpreter reports to a store observer: an
// instance field, a class static, and an array element.
public final class Stores {
    public int gold;
    public static int party;
    public int[] pack = new int[4];

    public void spend(int amount) {
        gold = amount;
        party = amount + 1;
        pack[2] = amount + 2;
    }
}
