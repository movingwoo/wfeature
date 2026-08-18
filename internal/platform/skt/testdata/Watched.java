// A class with two fields a watch can tell apart, written from two methods so
// a hit names the one that did it.
public final class Watched {
    public int gold;
    public int other;

    public void spend(int amount) {
        gold = amount;
    }

    public void elsewhere(int amount) {
        other = amount;
    }
}
