#!/usr/bin/env python3
"""Tests that the calculator refuses bars it cannot give a true answer for."""
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import emax_expected as m  # noqa: E402

NAME = "c-reversal"


def edit_times(lines, edit):
    """Applies edit(times) to the CSV's timestamps; the first two lines are headers."""
    head, rows = lines[:2], [r.split(",") for r in lines[2:]]
    times = [r[0] for r in rows]
    edit(times)
    return head + [",".join([t] + r[1:]) for t, r in zip(times, rows)]


def duplicate(times):
    times[4] = times[3]


def out_of_order(times):
    times[4], times[5] = times[5], times[4]


def intraday(times):
    times[4] = times[4].replace("T00:00:00Z", "T12:00:00Z")


def weekend(times):
    times[5] = "2010-01-09T00:00:00Z"  # a Saturday


class TimestampsTest(unittest.TestCase):
    def read_edited(self, edit):
        """Reads a scenario through read_bars after editing its CSV in a temp copy."""
        lines = m.csv_path(NAME).read_text().splitlines()
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(m, "HERE", Path(tmp)):
            path = m.csv_path(NAME)
            path.parent.mkdir(parents=True)
            path.write_text("\n".join(edit_times(lines, edit)) + "\n")
            return m.read_bars(NAME)

    def test_checked_in_scenarios_pass(self):
        for name in m.SCENARIOS:
            m.check_bars(name, m.read_bars(name))

    def test_bad_timestamps_are_refused(self):
        for edit in (duplicate, out_of_order, intraday, weekend):
            with self.subTest(edit.__name__):
                with self.assertRaisesRegex(AssertionError, "timestamp"):
                    m.check_bars(NAME, self.read_edited(edit))


if __name__ == "__main__":
    unittest.main()
