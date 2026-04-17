// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";
import {Counter} from "../src/Counter.sol";

contract CounterTest is Test {
    Counter public counter;

    address public alice;
    address public bob;

    function setUp() public {
        counter = new Counter();
        alice = makeAddr("alice");
        bob = makeAddr("bob");
    }

    // ──────────────────────── increment tests ─────────────────────────

    function test_increment_updatesCallerCount() public {
        vm.prank(alice);
        counter.increment();
        assertEq(counter.getCount(alice), 1);
    }

    function test_increment_multipleCallsAccumulate() public {
        vm.startPrank(alice);
        counter.increment();
        counter.increment();
        counter.increment();
        vm.stopPrank();
        assertEq(counter.getCount(alice), 3);
    }

    function test_increment_independentPerAccount() public {
        vm.prank(alice);
        counter.increment();

        vm.prank(bob);
        counter.increment();
        vm.prank(bob);
        counter.increment();

        assertEq(counter.getCount(alice), 1);
        assertEq(counter.getCount(bob), 2);
    }

    // ──────────────────────── setNumber tests ─────────────────────────

    function test_setNumber_updatesCallerCount() public {
        vm.prank(alice);
        counter.setNumber(42);
        assertEq(counter.getCount(alice), 42);
    }

    function test_setNumber_doesNotAffectOtherAccounts() public {
        vm.prank(alice);
        counter.setNumber(100);

        assertEq(counter.getCount(alice), 100);
        assertEq(counter.getCount(bob), 0);
    }

    function testFuzz_setNumber(uint256 x) public {
        vm.prank(alice);
        counter.setNumber(x);
        assertEq(counter.getCount(alice), x);
    }

    // ──────────────────────── getCount tests ──────────────────────────

    function test_getCount_returnsZeroForNewAccount() public view {
        assertEq(counter.getCount(alice), 0);
    }
}
