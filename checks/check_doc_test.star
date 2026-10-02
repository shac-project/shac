# Copyright 2026 The Shac Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

load("//doc/stdlib.star", doc_asserts = "asserts", doc_testing = "testing")
load("check_doc.star", "_struct_signature", "check_docs")

def test_check_docs():
    res = testing.run(check_docs)
    asserts.eq(res.findings, ())

def test_test_only_modules_documented():
    # asserts and testing aren't visible to check_docs because they're only
    # predeclared in *_test.star files, so their docs are validated here.
    asserts.eq(_struct_signature(doc_asserts), _struct_signature(asserts))
    asserts.eq(_struct_signature(doc_testing), _struct_signature(testing))

def test_struct_signature_private_helper():
    sample = struct(
        count = 1,
        fn = asserts.eq,
        nested = struct(name = "alice"),
    )
    asserts.eq(
        _struct_signature(sample),
        {
            "count": "int",
            "fn": "function",
            "nested": {
                "name": "string",
            },
        },
    )
