# Copyright 2023 The Shac Authors
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

def _hello_world(ctx):
    print("hello, world!")

check = shac.check(_hello_world)
print("str(check): %s" % str(check))
print("type(check): %s" % type(check))
print("bool(check): %s" % bool(check))

# The check object should be hashable. The `hash()` function doesn't work on
# arbitrary objects, only strings or bytes, so we can only assert hashability
# indirectly by trying to insert the check object into a set.
print("hashed: %s" % set([check]))
