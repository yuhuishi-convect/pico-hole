import struct
import unittest

from benchmarks import dns


def response_for(query: bytes, qtype: str, address: bytes) -> bytes:
    txid = struct.unpack_from("!H", query)[0]
    question_end = dns.skip_name(query, 12) + 4
    question = query[12:question_end]
    rr = b"\xc0\x0c" + struct.pack("!HHIH", dns.QTYPE[qtype], 1, 60, len(address)) + address
    return struct.pack("!HHHHHH", txid, 0x8180, 1, 1, 0, 0) + question + rr


class DNSBenchmarkTests(unittest.TestCase):
    def test_builds_query_for_rooted_name(self):
        packet = dns.encode_query("DoubleClick.NET.", "A", 0x1234)
        self.assertEqual(struct.unpack_from("!H", packet)[0], 0x1234)
        self.assertEqual(packet[-4:], struct.pack("!HH", dns.QTYPE["A"], 1))

    def test_accepts_blocked_sink_answers(self):
        for qtype, address in (("A", bytes(4)), ("AAAA", bytes(16))):
            with self.subTest(qtype=qtype):
                query = dns.encode_query("doubleclick.net", qtype, 0x44)
                dns.check_response(response_for(query, qtype, address), 0x44, qtype, "blocked")

    def test_rejects_wrong_sink_and_accepts_forwarded_address(self):
        query = dns.encode_query("example.com", "A", 0x45)
        response = response_for(query, "A", bytes((203, 0, 113, 8)))
        dns.check_response(response, 0x45, "A", "forwarded")
        with self.assertRaisesRegex(ValueError, "sink address"):
            dns.check_response(response, 0x45, "A", "blocked")
        sink = response_for(query, "A", bytes(4))
        with self.assertRaisesRegex(ValueError, "non-sink"):
            dns.check_response(sink, 0x45, "A", "forwarded")

    def test_rejects_wrong_transaction_id_and_truncated_data(self):
        query = dns.encode_query("example.com", "A", 0x46)
        response = response_for(query, "A", bytes((203, 0, 113, 8)))
        with self.assertRaisesRegex(ValueError, "transaction ID"):
            dns.decode_answers(response, 0x47, "A")
        with self.assertRaisesRegex(ValueError, "short DNS"):
            dns.decode_answers(b"\x00\x01", 1, "A")


if __name__ == "__main__":
    unittest.main()
