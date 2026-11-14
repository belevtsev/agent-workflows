"""Acceptance tests for the task `duration`. No arm of the benchmark saw this file.
Each case follows from the words of the task alone."""
import unittest

from calc import parse_duration


class Accepted(unittest.TestCase):
    def test_the_examples_of_the_task(self):
        self.assertEqual(parse_duration("1h30m"), 5400)
        self.assertEqual(parse_duration("45s"), 45)
        self.assertEqual(parse_duration("2d"), 172800)
        self.assertEqual(parse_duration("1d2h3m4s"), 93784)

    def test_the_result_is_an_int(self):
        self.assertIs(type(parse_duration("1h30m")), int)

    def test_units_may_be_left_out(self):
        self.assertEqual(parse_duration("1d4s"), 86404)
        self.assertEqual(parse_duration("2h5s"), 7205)
        self.assertEqual(parse_duration("10m"), 600)

    def test_numbers_of_several_digits(self):
        self.assertEqual(parse_duration("90m"), 5400)
        self.assertEqual(parse_duration("120s"), 120)


class Refused(unittest.TestCase):
    def refused(self, text):
        with self.assertRaises(ValueError, msg=repr(text)):
            parse_duration(text)

    def test_an_empty_text(self):
        self.refused("")

    def test_a_wrong_order(self):
        self.refused("30m1h")
        self.refused("4s1d")

    def test_a_unit_twice(self):
        self.refused("1h1h")
        self.refused("1s2s")

    def test_no_unit_or_no_number(self):
        self.refused("10")
        self.refused("h")
        self.refused("1h30")

    def test_other_text(self):
        self.refused("abc")
        self.refused("1w")
        self.refused("1.5h")
        self.refused("-1h")
        self.refused("1h 30m")


if __name__ == "__main__":
    unittest.main()
